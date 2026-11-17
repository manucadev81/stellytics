provider "hcloud" {
  # Token em HCLOUD_TOKEN.
}

# O Object Storage da Hetzner fala S3; o provider da AWS só cria o bucket.
provider "aws" {
  region                      = var.location
  access_key                  = var.object_storage_access_key
  secret_key                  = var.object_storage_secret_key
  skip_credentials_validation = true
  skip_region_validation      = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
  s3_use_path_style           = true

  endpoints {
    s3 = "https://${var.location}.your-objectstorage.com"
  }
}

locals {
  labels = {
    app        = "stellytics"
    managed-by = "opentofu"
  }
}

# A mudança de marca renomeia endereços de recursos já presentes no state.
# Sem estes mapeamentos, um `tofu apply` entenderia a VM e o firewall atuais
# como removidos e tentaria criar substitutos.
moved {
  from = hcloud_firewall.notify
  to   = hcloud_firewall.stellytics
}

moved {
  from = hcloud_server.notify
  to   = hcloud_server.stellytics
}

resource "hcloud_ssh_key" "admin" {
  # Preserve the remote object name during the branding migration.
  name       = "notify-admin"
  public_key = var.admin_ssh_public_key
  labels     = local.labels
}

# Só o Caddy (80 e 443) fica aberto para a internet. Postgres, Umami, portal e
# gateway não têm porta pública (infra/README.md, #62).
resource "hcloud_firewall" "stellytics" {
  name   = "notify"
  labels = local.labels

  rule {
    description = "SSH de administração"
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = var.admin_cidrs
  }

  rule {
    description = "HTTP (redireciona para HTTPS e valida o certificado)"
    direction   = "in"
    protocol    = "tcp"
    port        = "80"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }

  rule {
    description = "HTTPS"
    direction   = "in"
    protocol    = "tcp"
    port        = "443"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }

  rule {
    description = "HTTP/3"
    direction   = "in"
    protocol    = "udp"
    port        = "443"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }

  rule {
    description = "Ping"
    direction   = "in"
    protocol    = "icmp"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }
}

# Dados do Postgres e do Caddy num volume separado: sobrevivem se a VM for
# recriada. O Ansible monta em /srv/stellytics.
resource "hcloud_volume" "data" {
  name              = "notify-data"
  size              = var.data_volume_size
  location          = var.location
  format            = "ext4"
  delete_protection = true
  labels            = local.labels

  lifecycle {
    prevent_destroy = true
  }
}

resource "hcloud_server" "stellytics" {
  name         = "notify"
  server_type  = var.server_type
  image        = var.server_image
  location     = var.location
  ssh_keys     = [hcloud_ssh_key.admin.id]
  firewall_ids = [hcloud_firewall.stellytics.id]
  labels       = local.labels

  # Só o mínimo; o resto é do Ansible (infra/ansible).
  user_data = file("${path.module}/cloud-init.yaml")

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  delete_protection  = true
  rebuild_protection = true

  lifecycle {
    # Trocar o cloud-init não recria a VM: ele só roda na primeira subida.
    ignore_changes = [user_data, image, ssh_keys]
  }
}

resource "hcloud_volume_attachment" "data" {
  volume_id = hcloud_volume.data.id
  server_id = hcloud_server.stellytics.id
  automount = false
}

resource "aws_s3_bucket" "backup" {
  bucket = var.backup_bucket

  lifecycle {
    prevent_destroy = true
  }
}

# Monitoramento externo (#15, ADR 0005). Só SSH das redes de administração:
# Prometheus e Alertmanager escutam no loopback e são acessados por túnel.
resource "hcloud_firewall" "monitor" {
  name   = "notify-monitor"
  labels = local.labels

  rule {
    description = "SSH de administração"
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = var.admin_cidrs
  }

  rule {
    description = "Ping"
    direction   = "in"
    protocol    = "icmp"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }
}

resource "hcloud_server" "monitor" {
  name         = "notify-monitor"
  server_type  = var.monitor_server_type
  image        = var.server_image
  location     = var.monitor_location
  ssh_keys     = [hcloud_ssh_key.admin.id]
  firewall_ids = [hcloud_firewall.monitor.id]
  labels       = merge(local.labels, { role = "monitor" })
  user_data    = file("${path.module}/cloud-init.yaml")

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  lifecycle {
    precondition {
      condition     = var.monitor_location != var.location
      error_message = "O monitoramento precisa ficar em outro local da Hetzner, não no mesmo da produção."
    }
    ignore_changes = [user_data, image, ssh_keys]
  }
}

# ---------------------------------------------------------------------------
# Alta disponibilidade (#52, ADR 0009). Opt-in: com os padrões (0, false,
# false) nada abaixo é criado e o plano não muda. Só provisiona; não configura
# replicação, balanceamento nem o IP flutuante dentro da VM (Ansible, futuro).
# ---------------------------------------------------------------------------
locals {
  ha_enabled = var.ha_ingest_replicas > 0 || var.ha_db_standby
}

# Rede privada entre as VMs de HA, para o tráfego de replicação e de ingestão
# ao Postgres não passar pela internet.
resource "hcloud_network" "ha" {
  count    = local.ha_enabled ? 1 : 0
  name     = "notify-ha"
  ip_range = "10.50.0.0/16"
  labels   = local.labels
}

resource "hcloud_network_subnet" "ha" {
  count        = local.ha_enabled ? 1 : 0
  network_id   = hcloud_network.ha[0].id
  type         = "cloud"
  network_zone = "eu-central"
  ip_range     = "10.50.0.0/24"
}

resource "hcloud_server_network" "primary" {
  count     = local.ha_enabled ? 1 : 0
  server_id = hcloud_server.stellytics.id
  subnet_id = hcloud_network_subnet.ha[0].id
  ip        = "10.50.0.10"
}

# VMs extras de ingestão: mesmo firewall (só 80 e 443 abertos) da principal.
resource "hcloud_server" "ingest" {
  count        = var.ha_ingest_replicas
  name         = "notify-ingest-${count.index + 1}"
  server_type  = var.server_type
  image        = var.server_image
  location     = var.ha_ingest_location
  ssh_keys     = [hcloud_ssh_key.admin.id]
  firewall_ids = [hcloud_firewall.stellytics.id]
  labels       = merge(local.labels, { role = "ingest" })
  user_data    = file("${path.module}/cloud-init.yaml")

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  lifecycle {
    precondition {
      condition     = var.ha_ingest_location != var.location
      error_message = "As VMs extras de ingestão precisam ficar em outro local que a principal."
    }
    ignore_changes = [user_data, image, ssh_keys]
  }
}

resource "hcloud_server_network" "ingest" {
  count     = var.ha_ingest_replicas
  server_id = hcloud_server.ingest[count.index].id
  subnet_id = hcloud_network_subnet.ha[0].id
  ip        = "10.50.0.${20 + count.index}"
}

# Réplica do Postgres: VM e volume próprios, sem dados até o Ansible (futuro)
# configurar o standby com pg_basebackup a partir da principal.
resource "hcloud_volume" "standby" {
  count             = var.ha_db_standby ? 1 : 0
  name              = "notify-data-standby"
  size              = var.data_volume_size
  location          = var.ha_db_standby_location
  format            = "ext4"
  delete_protection = true
  labels            = local.labels

  lifecycle {
    precondition {
      condition     = var.ha_db_standby_location != var.location
      error_message = "A réplica do Postgres precisa ficar em outro local que a principal."
    }
    prevent_destroy = true
  }
}

resource "hcloud_server" "standby" {
  count        = var.ha_db_standby ? 1 : 0
  name         = "notify-db-standby"
  server_type  = var.server_type
  image        = var.server_image
  location     = var.ha_db_standby_location
  ssh_keys     = [hcloud_ssh_key.admin.id]
  firewall_ids = [hcloud_firewall.monitor.id] # só SSH e ping: o standby não atende a internet
  labels       = merge(local.labels, { role = "db-standby" })
  user_data    = file("${path.module}/cloud-init.yaml")

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  delete_protection  = true
  rebuild_protection = true

  lifecycle {
    precondition {
      condition     = var.ha_db_standby_location != var.location
      error_message = "A réplica do Postgres precisa ficar em outro local que a principal."
    }
    ignore_changes = [user_data, image, ssh_keys]
  }
}

resource "hcloud_volume_attachment" "standby" {
  count     = var.ha_db_standby ? 1 : 0
  volume_id = hcloud_volume.standby[0].id
  server_id = hcloud_server.standby[0].id
  automount = false
}

resource "hcloud_server_network" "standby" {
  count     = var.ha_db_standby ? 1 : 0
  server_id = hcloud_server.standby[0].id
  subnet_id = hcloud_network_subnet.ha[0].id
  ip        = "10.50.0.30"
}

# IP flutuante: o DNS aponta para ele e, numa falha da VM principal, a
# atribuição passa para outra VM (manual ou por script; sem failover
# automático aqui). Não vale para o DNS atual, que usa o IP da VM.
resource "hcloud_floating_ip" "ingest" {
  count         = var.ha_floating_ip ? 1 : 0
  type          = "ipv4"
  home_location = var.location
  name          = "notify-ingest"
  labels        = local.labels

  lifecycle {
    prevent_destroy = true
  }
}

resource "hcloud_floating_ip_assignment" "ingest" {
  count          = var.ha_floating_ip ? 1 : 0
  floating_ip_id = hcloud_floating_ip.ingest[0].id
  server_id      = hcloud_server.stellytics.id
}
