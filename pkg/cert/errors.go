package cert

type CaNotYetValidError struct {}

func (CaNotYetValidError) Error() string {
	return "The current CA is not yet valid"
}

type CaExpiredError struct {}

func (CaExpiredError) Error() string {
	return "The current CA is expired"
}
