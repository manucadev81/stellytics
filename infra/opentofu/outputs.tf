output "server_ipv4" {
  description = "IPv4 da VM: registros A de PORTAL_HOST, INGEST_HOST e API_HOST."
  value       = hcloud_server.notify.ipv4_address
}

output "server_ipv6" {
  description = "IPv6 da VM: registros AAAA de PORTAL_HOST, INGEST_HOST e API_HOST."
  value       = hcloud_server.notify.ipv6_address
}

output "data_volume_device" {
  description = "Dispositivo do volume de dados na VM (o Ansible monta em /srv/notify)."
  value       = "/dev/disk/by-id/scsi-0HC_Volume_${hcloud_volume.data.id}"
}

output "backup_s3_endpoint" {
  description = "Endpoint S3 do Object Storage, para WALG_S3_ENDPOINT."
  value       = "https://${var.location}.your-objectstorage.com"
}

output "backup_s3_prefix" {
  description = "Prefixo do WAL-G, para WALG_S3_PREFIX."
  value       = "s3://${aws_s3_bucket.backup.bucket}/postgres"
}

output "location" {
  description = "Local da Hetzner (data residency), para a documentação e o WAL-G."
  value       = var.location
}

output "monitor_ipv4" {
  description = "IPv4 da VM de monitoramento (inventário do Ansible, host `monitor`)."
  value       = hcloud_server.monitor.ipv4_address
}

# Alta disponibilidade (#52, ADR 0009): vazias com os padrões.
output "ingest_ipv4" {
  description = "IPv4 das VMs extras de ingestão (lista vazia se ha_ingest_replicas = 0)."
  value       = hcloud_server.ingest[*].ipv4_address
}

output "floating_ipv4" {
  description = "IPv4 flutuante (nulo se ha_floating_ip = false). Só IPv4: o registro AAAA continua apontando para a VM e não migra num failover."
  value       = one(hcloud_floating_ip.ingest[*].ip_address)
}

output "standby_private_ip" {
  description = "IP privado da réplica do Postgres (nulo se ha_db_standby = false)."
  value       = one(hcloud_server_network.standby[*].ip)
}
