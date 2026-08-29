package runner

type ConfigError struct {
	err error
}

func (e *ConfigError) Error() string {
	if e == nil || e.err == nil {
		return "config error"
	}
	return e.err.Error()
}

func (e *ConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}
