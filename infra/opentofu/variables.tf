# Valores não secretos ficam em production.tfvars (versionado). Os segredos
# vêm de variáveis de ambiente, nunca de arquivo no repositório:
#   HCLOUD_TOKEN                          token do projeto na Hetzner Cloud
#   TF_VAR_object_storage_access_key      credenciais do Object Storage
#   TF_VAR_object_storage_secret_key

variable "location" {
  description = "Local da Hetzner para a VM, o volume e o bucket (data residency: Alemanha)."
  type        = string
  default     = "nbg1"

  validation {
    # Locais da UE com Object Storage.
    condition     = contains(["nbg1", "fsn1", "hel1"], var.location)
    error_message = "Use um local da UE com Object Storage: nbg1, fsn1 ou hel1."
  }
}

variable "server_type" {
  description = "Tipo da VM. Confira os tipos disponíveis com `hcloud server-type list` antes do primeiro apply."
  type        = string
}

variable "server_image" {
  description = "Imagem do sistema da VM."
  type        = string
  default     = "debian-12"
}

variable "data_volume_size" {
  description = "Tamanho, em GB, do volume com os dados do Postgres."
  type        = number
  default     = 40

  validation {
    condition     = var.data_volume_size >= 10
    error_message = "O volume precisa de pelo menos 10 GB."
  }
}

variable "admin_ssh_public_key" {
  description = "Chave pública SSH de quem administra a VM."
  type        = string
}

variable "admin_cidrs" {
  description = "Redes que podem acessar o SSH (porta 22). Nunca 0.0.0.0/0."
  type        = list(string)

  validation {
    condition     = length(var.admin_cidrs) > 0 && alltrue([for c in var.admin_cidrs : !contains(["0.0.0.0/0", "::/0"], c)])
    error_message = "Informe ao menos uma rede de administração, sem 0.0.0.0/0 nem ::/0."
  }
}

variable "backup_bucket" {
  description = "Nome do bucket do backup do Postgres no Object Storage (único em toda a Hetzner)."
  type        = string
}

variable "object_storage_access_key" {
  description = "Chave de acesso do Object Storage (criada no console da Hetzner)."
  type        = string
  sensitive   = true
}

variable "object_storage_secret_key" {
  description = "Segredo do Object Storage (criado no console da Hetzner)."
  type        = string
  sensitive   = true
}

# Monitoramento externo (#15, ADR 0005): VM separada, em outro data center,
# para continuar avisando se a VM de produção cair inteira.
variable "monitor_location" {
  description = "Local da VM de monitoramento; diferente de `location`, para não cair junto."
  type        = string
  default     = "fsn1"

  validation {
    condition     = contains(["nbg1", "fsn1", "hel1"], var.monitor_location)
    error_message = "Use um local da UE: nbg1, fsn1 ou hel1."
  }
}

variable "monitor_server_type" {
  description = "Tipo da VM de monitoramento (a menor serve). Confira com `hcloud server-type list`."
  type        = string
}

# Alta disponibilidade (#52, ADR 0009). Tudo desligado por padrão: com os
# valores padrão o plano é idêntico ao anterior. Estes recursos são só a base
# de provisionamento; a replicação do Postgres e o IP flutuante ainda não têm
# failover automatizado no Ansible. As VMs extras usam Caddy. Ver docs/sla.md.
variable "ha_ingest_replicas" {
  description = "Quantidade de VMs extras de ingestão (gateway, coletor e Caddy), além da principal. 0 desliga."
  type        = number
  default     = 0

  validation {
    condition     = var.ha_ingest_replicas >= 0 && var.ha_ingest_replicas <= 3 && floor(var.ha_ingest_replicas) == var.ha_ingest_replicas
    error_message = "Use um inteiro de 0 a 3."
  }
}

variable "ha_ingest_location" {
  description = "Local das VMs extras de ingestão. Diferente de `location`, para não cair junto; precisa estar na mesma zona de rede da Hetzner (eu-central) da produção."
  type        = string
  default     = "fsn1"

  validation {
    condition     = contains(["nbg1", "fsn1"], var.ha_ingest_location)
    error_message = "Use nbg1 ou fsn1 (mesma zona de rede eu-central da Hetzner)."
  }
}

variable "ha_floating_ip" {
  description = "Cria um IP flutuante (IPv4) apontado para a VM principal, para trocar de VM sem mudar o DNS. false desliga."
  type        = bool
  default     = false
}

variable "ha_db_standby" {
  description = "Cria a VM e o volume de uma réplica do Postgres (standby). A replicação em si não é configurada pela IaC ainda. false desliga."
  type        = bool
  default     = false
}

variable "ha_db_standby_location" {
  description = "Local da réplica do Postgres. Mesma zona de rede da produção (nbg1 ou fsn1)."
  type        = string
  default     = "fsn1"

  validation {
    condition     = contains(["nbg1", "fsn1"], var.ha_db_standby_location)
    error_message = "Use nbg1 ou fsn1 (mesma zona de rede eu-central da Hetzner)."
  }
}
