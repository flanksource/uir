package indexer

import "github.com/flanksource/uir"

type targetLookupKey struct {
	RootKey     string
	IdentityKey string
}

type targetIndex struct {
	exact    map[targetLookupKey][]persistedTarget
	callable map[targetLookupKey][]persistedTarget
}

func (index *targetIndex) add(target persistedTarget) {
	if index.exact == nil {
		index.exact = make(map[targetLookupKey][]persistedTarget)
		index.callable = make(map[targetLookupKey][]persistedTarget)
	}
	exact := target.ID.IdentityKey()
	callable := callableIdentityKey(target.ID)
	for _, root := range []string{"", target.RootKey} {
		exactKey := targetLookupKey{RootKey: root, IdentityKey: exact}
		callableKey := targetLookupKey{RootKey: root, IdentityKey: callable}
		index.exact[exactKey] = append(index.exact[exactKey], target)
		index.callable[callableKey] = append(index.callable[callableKey], target)
	}
}

func (index *targetIndex) resolve(call callSpec) (persistedTarget, bool) {
	root := ""
	if call.ToRootKey != nil {
		root = *call.ToRootKey
	}
	candidates := index.exact[targetLookupKey{RootKey: root, IdentityKey: call.ToIdentifier.IdentityKey()}]
	if len(candidates) == 0 && call.ToIdentifier.Signature == "" {
		candidates = index.callable[targetLookupKey{RootKey: root, IdentityKey: callableIdentityKey(call.ToIdentifier)}]
	}
	if len(candidates) != 1 {
		return persistedTarget{}, false
	}
	return candidates[0], true
}

func callableIdentityKey(identifier uir.Identifier) string {
	identifier.Signature = ""
	return identifier.IdentityKey()
}
