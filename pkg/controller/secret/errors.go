package secret

type MissingCaError struct {
}

func (MissingCaError) Error() string {
	return "CA has not been created yet, please generate a new"
}
