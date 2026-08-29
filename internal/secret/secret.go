package secret

import "encoding/json"

type Secret struct {
	name  string
	value string
}

func New(name, value string) Secret {
	return Secret{name: name, value: value}
}

func (s Secret) Name() string {
	return s.name
}

func (s Secret) String() string {
	return "[redacted]"
}

func (s Secret) GoString() string {
	return "secret.Secret{[redacted]}"
}

func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal("[redacted]")
}

func (s Secret) MarshalYAML() (any, error) {
	return "[redacted]", nil
}

func (s Secret) Reveal() string {
	return s.value
}

func (s Secret) IsZero() bool {
	return s.name == "" && s.value == ""
}
