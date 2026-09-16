package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/asymmetric/nixpkgs-update-notifier/regexes"
)

// packageMeta is a minimal view of a package's entry in packages.json,
// sufficient to extract its maintainers' GitHub handles.
type packageMeta struct {
	Meta struct {
		Maintainers []struct {
			GitHub string `json:"github"`
		} `json:"maintainers"`
	} `json:"meta"`
}

// buildMaintainerIndex streams packages.json (as decoded from packages.json.br)
// and builds an inverted index from lowercased GitHub handle to the sorted,
// deduplicated list of normalized attr paths maintained by that handle.
//
// It avoids ever holding the whole document in memory: only the top-level
// "packages" object is walked key by key, and each package's JSON is decoded
// into a minimal struct before being discarded.
func buildMaintainerIndex(r io.Reader) (map[string][]string, error) {
	dec := json.NewDecoder(r)

	// consume opening '{' of the top-level object
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("reading opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected top-level object, got %v", tok)
	}

	index := make(map[string]map[string]struct{})

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("reading top-level key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", keyTok)
		}

		if key != "packages" {
			// skip this value, whatever it is
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("skipping top-level key %q: %w", key, err)
			}
			continue
		}

		if err := decodePackages(dec, index); err != nil {
			return nil, fmt.Errorf("decoding packages: %w", err)
		}
	}

	// consume closing '}' of the top-level object
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("reading closing token: %w", err)
	}

	result := make(map[string][]string, len(index))
	for handle, attrPaths := range index {
		aps := make([]string, 0, len(attrPaths))
		for ap := range attrPaths {
			aps = append(aps, ap)
		}
		sort.Strings(aps)
		result[handle] = aps
	}

	return result, nil
}

// decodePackages walks the "packages" object's value, populating index.
func decodePackages(dec *json.Decoder, index map[string]map[string]struct{}) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("reading opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected object, got %v", tok)
	}

	for dec.More() {
		attrPathTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("reading attr path key: %w", err)
		}
		attrPath, ok := attrPathTok.(string)
		if !ok {
			return fmt.Errorf("expected string key, got %v", attrPathTok)
		}

		// Fresh zero value each iteration: encoding/json merges into existing
		// values rather than resetting them, so reusing a variable across
		// iterations would leak maintainers from one package into the next.
		var pkg packageMeta
		if err := dec.Decode(&pkg); err != nil {
			return fmt.Errorf("decoding package %q: %w", attrPath, err)
		}

		normalized := regexes.NormalizeAttrPath(attrPath)
		for _, m := range pkg.Meta.Maintainers {
			if m.GitHub == "" {
				continue
			}
			handle := strings.ToLower(m.GitHub)
			if index[handle] == nil {
				index[handle] = make(map[string]struct{})
			}
			index[handle][normalized] = struct{}{}
		}
	}

	// consume closing '}' of the packages object
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("reading closing token: %w", err)
	}

	return nil
}
