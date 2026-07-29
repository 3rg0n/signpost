package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseJSON parses a JSON document into a Node tree.
//
// Built on `encoding/json`'s streaming Decoder rather than on `Unmarshal` into
// `map[string]any`, for one reason: a Go map loses key order, and key order is a
// fact worth keeping. `package.json`'s `scripts` block reads as a list a human wrote
// in a deliberate order, and the emitter's byte-stability requirement (design §8.1)
// is easier to satisfy when the reader never introduced nondeterminism in the first
// place.
//
// Numbers are kept as their original text via Decoder.UseNumber. A version pin of
// `1.10` must not come back as `1.1`, and a large integer must not acquire an
// exponent.
//
// Errors are returned rather than swallowed, unlike the YAML and TOML readers, and
// the difference is deliberate: JSON has no tolerated-dialect problem. There is no
// JSON equivalent of a Helm template, so a `package.json` that does not parse is
// genuinely malformed — probably mid-merge-conflict — and the honest report is that
// it could not be read, not a partial tree.
func ParseJSON(src string) (*Node, error) {
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("empty document")
		}
		return nil, err
	}
	n, err := decodeJSON(dec, tok)
	if err != nil {
		return nil, err
	}
	// Trailing content means the file holds more than one document, which no
	// manifest format signpost reads permits.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected content after the top-level value")
	}
	return n, nil
}

// decodeJSON builds a Node from an already-read token and the decoder's remainder.
func decodeJSON(dec *json.Decoder, tok json.Token) (*Node, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := mapNode(0)
			for {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				if d, ok := keyTok.(json.Delim); ok && d == '}' {
					return n, nil
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				valTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := decodeJSON(dec, valTok)
				if err != nil {
					return nil, err
				}
				n.set(key, val)
			}
		case '[':
			n := seqNode(0)
			for {
				itemTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				if d, ok := itemTok.(json.Delim); ok && d == ']' {
					return n, nil
				}
				item, err := decodeJSON(dec, itemTok)
				if err != nil {
					return nil, err
				}
				n.Items = append(n.Items, item)
			}
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	case string:
		return scalarNode(t, true, 0), nil
	case json.Number:
		return scalarNode(t.String(), false, 0), nil
	case bool:
		if t {
			return scalarNode("true", false, 0), nil
		}
		return scalarNode("false", false, 0), nil
	case nil:
		return scalarNode("", false, 0), nil
	}
	return nil, fmt.Errorf("unexpected token %T", tok)
}
