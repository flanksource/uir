package query

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the least recently used cache", func() {
	present := func(cache *lru[string, int], keys ...string) []string {
		var found []string
		for _, key := range keys {
			if _, ok := cache.get(key); ok {
				found = append(found, key)
			}
		}
		return found
	}

	It("evicts the entry used longest ago once it is full", func() {
		cache := newLRU[string, int](2)
		cache.put("first", 1)
		cache.put("second", 2)
		value, found := cache.get("first")
		Expect([]any{value, found}).To(Equal([]any{1, true}))
		cache.put("third", 3)
		Expect(present(cache, "first", "second", "third")).To(Equal([]string{"first", "third"}))
	})

	It("replaces the value of a key it holds without evicting another", func() {
		cache := newLRU[string, int](2)
		cache.put("first", 1)
		cache.put("second", 2)
		cache.put("first", 10)
		value, _ := cache.get("first")
		Expect(value).To(Equal(10))
		Expect(present(cache, "first", "second")).To(Equal([]string{"first", "second"}))
	})

	It("keeps nothing with no capacity", func() {
		cache := newLRU[string, int](0)
		cache.put("first", 1)
		Expect(present(cache, "first")).To(BeEmpty())
	})
})
