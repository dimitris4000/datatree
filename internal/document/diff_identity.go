package document

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const diffWorkLimit = 64 * 1024 * 1024

type diffWork struct{ left int }

func (w *diffWork) spend(n int) error {
	w.left -= n
	if w.left < 0 {
		return errors.New("structural diff work limit exceeded")
	}
	return nil
}
func diffHash(parts ...string) string {
	h := sha256.New()
	var size [8]byte
	for _, p := range parts {
		binary.BigEndian.PutUint64(size[:], uint64(len(p)))
		h.Write(size[:])
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type diffIdentityKey struct {
	node  *yaml.Node
	typed bool
	scope *yaml.Node
}
type diffAddressKey struct{ node, scope *yaml.Node }

type diffParent struct {
	node  *yaml.Node
	index int
}
type diffIdentity struct {
	root            *yaml.Node
	parents         map[*yaml.Node]diffParent
	hashes          map[diffIdentityKey]string
	addresses       map[diffAddressKey]string
	activeHashes    map[diffIdentityKey]bool
	activeAddresses map[diffAddressKey]bool
	work            *diffWork
}

func newDiffIdentity(d *Document, w *diffWork) *diffIdentity {
	i := &diffIdentity{root: d.raw, parents: map[*yaml.Node]diffParent{}, hashes: map[diffIdentityKey]string{}, addresses: map[diffAddressKey]string{}, activeHashes: map[diffIdentityKey]bool{}, activeAddresses: map[diffAddressKey]bool{}, work: w}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		for j, c := range n.Content {
			i.parents[c] = diffParent{n, j}
			walk(c)
		}
	}
	walk(d.raw)
	return i
}

// A reference within a complex key uses a location relative to that key.
// This gives cyclic keys a finite identity without a provisional global address.
func (i *diffIdentity) inside(scope, n *yaml.Node) (bool, error) {
	if scope == nil {
		return false, nil
	}
	for n != nil {
		if err := i.work.spend(1); err != nil {
			return false, err
		}
		if n == scope {
			return true, nil
		}
		parent, ok := i.parents[n]
		if !ok {
			break
		}
		n = parent.node
	}
	return false, nil
}
func (i *diffIdentity) address(n, scope *yaml.Node) (string, error) {
	if n == nil {
		return "", errors.New("alias has no structural target")
	}
	k := diffAddressKey{n, scope}
	if a, ok := i.addresses[k]; ok {
		return a, nil
	}
	if err := i.work.spend(1); err != nil {
		return "", err
	}
	if i.activeAddresses[k] {
		return diffHash("address-back-edge"), nil
	}
	i.activeAddresses[k] = true
	defer delete(i.activeAddresses, k)
	if n == scope || (scope == nil && n == i.root) {
		marker := "root"
		if scope != nil {
			marker = "key-root"
		}
		i.addresses[k] = diffHash(marker)
		return i.addresses[k], nil
	}
	p, ok := i.parents[n]
	if !ok {
		return "", errors.New("alias target is outside the document")
	}
	parent, err := i.address(p.node, scope)
	if err != nil {
		return "", err
	}
	var result string
	if p.node.Kind == yaml.MappingNode {
		rawKey := p.node.Content[p.index/2*2]
		keyScope := scope
		if keyScope == nil {
			keyScope = rawKey
		}
		key, err := i.hashScoped(rawKey, true, keyScope)
		if err != nil {
			return "", err
		}
		result = diffHash(parent, "entry", key, strconv.Itoa(p.index%2))
	} else {
		result = diffHash(parent, "index", strconv.Itoa(p.index))
	}
	i.addresses[k] = result
	return result, nil
}
func (i *diffIdentity) hash(n *yaml.Node, typed bool) (string, error) {
	var scope *yaml.Node
	if typed {
		scope = n
	}
	return i.hashScoped(n, typed, scope)
}
func (i *diffIdentity) hashScoped(n *yaml.Node, typed bool, scope *yaml.Node) (string, error) {
	k := diffIdentityKey{n, typed, scope}
	if h, ok := i.hashes[k]; ok {
		return h, nil
	}
	if err := i.work.spend(1 + len(n.Value) + len(n.Tag)); err != nil {
		return "", err
	}
	if i.activeHashes[k] {
		return diffHash("key-back-edge", strconv.Itoa(int(n.Kind)), n.Tag), nil
	}
	i.activeHashes[k] = true
	defer delete(i.activeHashes, k)
	tag := n.Tag
	parts := []string{strconv.Itoa(int(n.Kind)), tag}
	switch n.Kind {
	case yaml.AliasNode:
		local, err := i.inside(scope, n.Alias)
		if err != nil {
			return "", err
		}
		var targetScope *yaml.Node
		if local {
			targetScope = scope
		}
		address, err := i.address(n.Alias, targetScope)
		if err != nil {
			return "", err
		}
		parts = []string{"alias", address}
	case yaml.MappingNode:
		pairs := []string{}
		for j := 0; j < len(n.Content); j += 2 {
			keyScope := scope
			if !typed {
				keyScope = n.Content[j]
			}
			key, err := i.hashScoped(n.Content[j], true, keyScope)
			if err != nil {
				return "", err
			}
			value, err := i.hashScoped(n.Content[j+1], typed, scope)
			if err != nil {
				return "", err
			}
			pairs = append(pairs, diffHash(key, value))
		}
		sort.Strings(pairs)
		parts = append(parts, pairs...)
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range n.Content {
			h, err := i.hashScoped(c, typed, scope)
			if err != nil {
				return "", err
			}
			parts = append(parts, h)
		}
	default:
		value := n.Value
		switch tag {
		case "!!int", "!!float":
			if len(value) > MaxFindText {
				return "", errors.New("structural diff numeric comparison limit is 4096 bytes per value")
			}
			if !typed {
				parts[1] = "number"
			}
			special := strings.ToLower(strings.TrimPrefix(value, "+"))
			if special == ".inf" || special == "-.inf" || special == ".nan" {
				value = special
			} else {
				normalized, ok := queryNumberText(&Node{raw: n, Value: value})
				if !ok {
					return "", fmt.Errorf("cannot compare numeric value %q", shortPreview(value))
				}
				dec, ok := decimalNumber(normalized)
				if !ok {
					return "", fmt.Errorf("cannot compare numeric value %q", shortPreview(value))
				}
				value = fmt.Sprintf("%t:%s:%s", dec.negative, dec.digits, dec.magnitude.String())
			}
		case "!!bool":
			switch strings.ToLower(value) {
			case "true", "yes", "on":
				value = "true"
			default:
				value = "false"
			}
		case "!!null":
			value = ""
		}
		parts = append(parts, value)
	}
	h := diffHash(parts...)
	i.hashes[k] = h
	return h, nil
}
