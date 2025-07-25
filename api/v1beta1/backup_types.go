package v1beta1

// Finalizer added to CNPG Backup resources managed by plugin
// It is used to ensure that backup removed from storage before Backup resource deleted
const BackupFinalizerName = "cnpg-plugin-wal-g.yandex.cloud/backup-cleanup"

// Labels added to CNPG Backup resources managed by plugin:

// PGVersionLabel represents PG major version from which backup was created
const BackupPgVersionLabelName = "cnpg-plugin-wal-g.yandex.cloud/pg-major"

// BackupTypeLabel represents type of created backup (full / incremental)
const BackupTypeLabelName = "cnpg-plugin-wal-g.yandex.cloud/backup-type"

// Annotations added to CNPG Backup resources managed by plugin:

// BackupAllDependentsAnnotation represents all incremental backups (both direct and indirect)
// depending on current backup
const BackupAllDependentsAnnotationName = "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-all"

// BackupDirectDependentsAnnotation represents incremental backups which are created from current backup
const BackupDirectDependentsAnnotationName = "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-direct"

// BackupType is string enumeration for backup types, can only be "full" or "incremental"
type BackupType string

const BackupTypeFull BackupType = "full"
const BackupTypeIncremental BackupType = "incremental"
