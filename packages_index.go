package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	json "encoding/json/v2"

	"github.com/asymmetric/nixpkgs-update-notifier/regexes"
)

// maintainerIndex maps a lowercased GitHub handle to the sorted, normalized attr paths of packages it maintains.
type maintainerIndex map[string][]string

func (index maintainerIndex) packages(handle string) []string {
	return index[strings.ToLower(handle)]
}

// insert avoids inserting a package twice for the same maintainer (and coincidentally, keeps the list of packages sorted).
func (index maintainerIndex) insert(handle, packageName string) {
	if handle == "" {
		return
	}

	h := strings.ToLower(handle)
	attrPaths := index[h]
	attrPath := regexes.NormalizeAttrPath(packageName)
	// avoid inserting duplicates
	i, found := slices.BinarySearch(attrPaths, attrPath)
	if found {
		return
	}

	index[h] = slices.Insert(attrPaths, i, attrPath)
}

// buildMaintainerIndex reads packages.json and returns an index from lowercased
// GitHub handle to the sorted, deduplicated, normalized attr paths that handle
// maintains.
//
// json.UnmarshalRead streams r, and only the fields declared on doc are kept
// (unknown members are skipped), so what is kept in memory is only the doc struct.
// We could avoid that too by using jsontext, but it's not worth it.
// Duplicate object names are rejected as an error by json/v2.
//
// We use json/v2 because it gives us ergonomic streaming primitives.
func buildMaintainerIndex(r io.Reader) (maintainerIndex, error) {
	// Projection of .packages[name].meta.maintainers[].github
	var doc struct {
		Packages map[string]struct {
			Meta struct {
				Maintainers []struct {
					GitHub string `json:"github"`
				} `json:"maintainers"`
			} `json:"meta"`
		} `json:"packages"`
	}
	if err := json.UnmarshalRead(r, &doc); err != nil {
		return nil, fmt.Errorf("decoding packages.json: %w", err)
	}

	// handle -> []package
	index := make(maintainerIndex)
	for name, pkg := range doc.Packages {
		for _, m := range pkg.Meta.Maintainers {
			index.insert(m.GitHub, name)
		}
	}

	return index, nil
}
