package format

import (
	"encoding/json"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// ReplySchema is the JSON schema passed to `claude --json-schema`. It is
// generated from the Reply type, so the format is defined in one place. Lists
// are made non-nullable (jsonschema-go allows null for Go slices) and the
// enums for kind and code_lang are filled in from the Go constants.
func ReplySchema() ([]byte, error) {
	s, err := jsonschema.For[Reply](nil)
	if err != nil {
		return nil, err
	}
	nonNullLists(s)
	q := s.Properties["questions"].Items
	q.Properties["kind"].Enum = []any{string(Single), string(Multi)}
	langs := make([]any, len(CodeLangs))
	for i, l := range CodeLangs {
		langs[i] = l
	}
	q.Properties["code_lang"].Enum = langs
	return json.Marshal(s)
}

func nonNullLists(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) == 2 && slices.Contains(s.Types, "null") && slices.Contains(s.Types, "array") {
		s.Types = nil
		s.Type = "array"
	}
	for _, p := range s.Properties {
		nonNullLists(p)
	}
	nonNullLists(s.Items)
}
