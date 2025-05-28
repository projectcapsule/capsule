package errors

type ItemProcessingError struct {
	Err error
}

func (e *ItemProcessingError) Error() string {
	return e.Err.Error()
}

func (e *ItemProcessingError) Unwrap() error {
	return e.Err
}
