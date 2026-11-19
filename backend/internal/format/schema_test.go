package format

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestReplySchemaShape(t *testing.T) {
	raw, err := ReplySchema()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "$ref") {
		t.Fatal("the schema Claude gets must be self-contained")
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	req := toStrings(s["required"])
	sort.Strings(req)
	if want := []string{"new_mistakes", "new_topics", "questions", "reply"}; !reflect.DeepEqual(req, want) {
		t.Fatalf("top-level required = %v, want %v", req, want)
	}
	props := s["properties"].(map[string]any)
	questions := props["questions"].(map[string]any)
	if questions["type"] != "array" {
		t.Fatalf("questions.type = %v, want array (never null)", questions["type"])
	}
	qprops := questions["items"].(map[string]any)["properties"].(map[string]any)
	if got := toStrings(qprops["kind"].(map[string]any)["enum"]); !reflect.DeepEqual(got, []string{"single", "multi"}) {
		t.Fatalf("kind enum = %v", got)
	}
	if got := toStrings(qprops["code_lang"].(map[string]any)["enum"]); !reflect.DeepEqual(got, CodeLangs) {
		t.Fatalf("code_lang enum = %v, want %v", got, CodeLangs)
	}
	walkObjects(t, s, "$")
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// walkObjects checks every object node forbids extra properties and that no
// array is nullable.
func walkObjects(t *testing.T, node map[string]any, path string) {
	t.Helper()
	if node["type"] == "object" {
		if ap, ok := node["additionalProperties"]; !ok || ap != false {
			t.Fatalf("%s: additionalProperties = %v, want false", path, ap)
		}
	}
	if types, ok := node["type"].([]any); ok {
		t.Fatalf("%s: type is %v; lists must not be nullable", path, types)
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for k, v := range props {
			walkObjects(t, v.(map[string]any), path+"."+k)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		walkObjects(t, items, path+"[]")
	}
}
