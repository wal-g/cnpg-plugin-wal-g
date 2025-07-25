package v1beta1

const (
	DefaultDownloadFileRetries = 5
	DefaultDeltaMaxSteps       = 0
)

func (b *BackupConfig) Default() {
	if b.Spec.DownloadFileRetries == 0 {
		b.Spec.DownloadFileRetries = DefaultDownloadFileRetries
	}
	if b.Spec.DeltaMaxSteps == 0 {
		b.Spec.DeltaMaxSteps = DefaultDeltaMaxSteps
	}
}
