package meta

const (
	CapsuleFieldOwnerPrefix = "capsule"
)

func ControllerFieldOwnerPrefix(fieldowner string) string {
	return CapsuleFieldOwnerPrefix + "/" + fieldowner
}
