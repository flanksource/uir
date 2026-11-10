package query_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/effects"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	effectsModule = "example.org/effects"
	effectsSource = `package effects

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	counter  int
	greeting = "hello"
	started  = time.Now()
	cache    = map[string]int{}
	mu       sync.Mutex
	hook     func() int
	word     = regexp.MustCompile("^[a-z]+$")
	registry = &Registry{}
)

type Registry struct{ names []string }

func (r *Registry) Insert(name string) { r.names = append(r.names, name) }

func (r Registry) Len() int { return len(r.names) }

type Store struct{ db *sql.DB }

type Shape interface{ Area() float64 }

func Add(a, b int) int { return a + b }

func Greet(name string) string { return greeting + ", " + strings.ToUpper(name) }

func Shout(name string) string { return Greet(name) + "!" }

func IsWord(s string) bool { return word.MatchString(s) }

func Sorted(xs []string) []string { slices.Sort(xs); return xs }

func Factorial(n int) int {
	if n <= 1 {
		return 1
	}
	return n * Factorial(n-1)
}

func Even(n int) bool {
	if n == 0 {
		return true
	}
	return Odd(n - 1)
}

func Odd(n int) bool {
	if n == 0 {
		return false
	}
	return Even(n - 1)
}

func Locked(key string) int {
	mu.Lock()
	defer mu.Unlock()
	return len(key)
}

func Keys(m map[string]int) []string {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func Now() time.Time { return time.Now() }

func Uptime() time.Duration { return time.Duration(len(started.String())) }

func Ping(n int) {
	if n > 0 {
		Pong(n - 1)
	}
}

func Pong(n int) {
	_ = time.Now()
	Ping(n)
}

func Home() string { return os.Getenv("HOME") }

func Load(path string) ([]byte, error) { return os.ReadFile(path) }

func Report(path string) (int, error) {
	data, err := Load(path)
	return len(data), err
}

func Print(x int) { fmt.Println(x) }

func (s Store) Save(name string) error {
	_, err := s.db.Exec("INSERT INTO names VALUES (?)", name)
	return err
}

func (s Store) Count() (n int, err error) {
	err = s.db.QueryRow("SELECT count(*) FROM names").Scan(&n)
	return n, err
}

func Bump() { counter++ }

func Current() int { return counter }

func Remember(key string, value int) { cache[key] = value }

func Register(name string) { registry.Insert(name) }

func Registered() int { return registry.Len() }

func Spawn(f func()) { go f() }

func Send(ch chan int) { ch <- 1 }

func Call(f func() int) int { return f() }

func Hooked() int { return hook() }

func Measure(s Shape) float64 { return s.Area() }
`
)

func effectsWorkspace() string {
	GinkgoHelper()
	checkout := filepath.Join(GinkgoT().TempDir(), "effects")
	Expect(os.MkdirAll(checkout, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+effectsModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "effects.go"), []byte(effectsSource), 0o644)).To(Succeed())
	return checkout
}

var _ = Describe("symbol effects", Ordered, ContinueOnFailure, func() {
	var (
		pipeline *query.Pipeline
		scope    query.ModuleScopeOptions
	)

	BeforeAll(func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := effectsWorkspace()
		indexCheckout(ctx, database, checkout)
		var err error
		pipeline, err = query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope = query.ModuleScopeOptions{RootKey: effectsModule, Location: checkout}
	})

	symbolEffects := func(ctx SpecContext, selector string) query.EffectsResult {
		GinkgoHelper()
		result, err := pipeline.Effects(ctx, query.EffectsOptions{Selector: selector, Scope: scope})
		Expect(err).ToNot(HaveOccurred(), selector)
		Expect(result.Candidates).To(BeEmpty(), selector)
		return result
	}

	DescribeTable("finds each symbol's effects, through what it calls and reads",
		func(ctx SpecContext, selector string, deterministic bool, expected ...effects.Effect) {
			result := symbolEffects(ctx, selector)
			found := []effects.Effect{}
			for _, effect := range result.Effects {
				found = append(found, effect.Effect)
			}
			Expect(found).To(Equal(append([]effects.Effect{}, expected...)), selector)
			Expect(result.Deterministic).To(Equal(deterministic), selector)
		},
		Entry("arithmetic", "func:Add", true),
		Entry("a package variable nothing writes, and a computing library call", "func:Greet", true),
		Entry("a deterministic callee", "func:Shout", true),
		Entry("a compiled expression's read-only method", "func:IsWord", true),
		Entry("a library call that sorts in place", "func:Sorted", true),
		Entry("recursion", "func:Factorial", true),
		Entry("mutual recursion", "func:Even", true),
		Entry("a lock alone", "func:Locked", true, effects.LockWrite),
		Entry("map iteration alone", "func:Keys", true, effects.MapOrder),
		Entry("the clock", "func:Now", false, effects.TimeRead),
		Entry("a variable whose initializer reads the clock", "func:Uptime", false, effects.TimeRead),
		Entry("that variable itself", "var:started", false, effects.TimeRead),
		Entry("a cycle one member of which reads the clock", "func:Ping", false, effects.TimeRead),
		Entry("the environment", "func:Home", false, effects.EnvRead),
		Entry("a file read two calls away", "func:Report", false, effects.FileRead),
		Entry("standard output", "func:Print", false, effects.FileWrite),
		Entry("a database write", "method:Store.Save", false, effects.DBWrite),
		Entry("a database read", "method:Store.Count", false, effects.DBRead),
		Entry("a package variable write", "func:Bump", false, effects.GlobalWrite),
		Entry("a package variable another function writes", "func:Current", false, effects.GlobalRead),
		Entry("a store into a package map", "func:Remember", false, effects.GlobalWrite),
		Entry("a pointer method on a package variable", "func:Register", false, effects.GlobalWrite),
		Entry("a value method on a package variable a pointer method mutates", "func:Registered", false, effects.GlobalRead),
		Entry("a goroutine of a function value", "func:Spawn", false, effects.Goroutine, effects.Unknown),
		Entry("a channel send", "func:Send", false, effects.Channel),
		Entry("a function value parameter", "func:Call", false, effects.Unknown),
		Entry("a function-valued package variable", "func:Hooked", false, effects.Unknown),
		Entry("a call of an interface method", "func:Measure", false, effects.Unknown),
		Entry("an interface method", "method:Shape.Area", false, effects.Unknown),
	)

	It("explains an effect by the shortest path to the symbol that has it", func(ctx SpecContext) {
		Expect(symbolEffects(ctx, "func:Report").Effects).To(Equal([]query.SymbolEffect{
			{Effect: effects.FileRead, Path: []string{effectsModule + ".Report", effectsModule + ".Load", "os.ReadFile"}, Evidence: "os.ReadFile"},
		}))
		Expect(symbolEffects(ctx, "func:Ping").Effects).To(Equal([]query.SymbolEffect{
			{Effect: effects.TimeRead, Path: []string{effectsModule + ".Ping", effectsModule + ".Pong", "time.Now"}, Evidence: "time.Now"},
		}))
		Expect(symbolEffects(ctx, "func:Spawn").Effects).To(ContainElement(
			query.SymbolEffect{Effect: effects.Goroutine, Path: []string{effectsModule + ".Spawn"}, Evidence: "go f"}))
	})

	It("selects deterministic symbols in SQL", func(ctx SpecContext) {
		result := runQuery(ctx, pipeline, "SELECT symbol.name FROM func:* WHERE symbol.deterministic = TRUE ORDER BY symbol.name", scope)
		var names []any
		for _, row := range result.Rows {
			names = append(names, row["symbol.name"])
		}
		Expect(names).To(Equal([]any{"Add", "Even", "Factorial", "Greet", "Insert", "IsWord", "Keys", "Len", "Locked", "Odd", "Shout", "Sorted"}))
		result = runQuery(ctx, pipeline, "SELECT symbol.effects FROM func:Spawn", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.effects": "goroutine,unknown"}}))
	})

	It("claims nothing for a document indexed before effects were recorded", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := effectsWorkspace()
		indexCheckout(ctx, database, checkout)
		var document storage.Document
		Expect(database.Where("path_key = ?", "effects.go").Take(&document).Error).To(Succeed())
		var content storage.DocumentContent
		Expect(json.Unmarshal(document.Content, &content)).To(Succeed())
		content.Version, content.EffectsRecorded = 11, false
		for i := range content.Symbols {
			content.Symbols[i].Effects = nil
		}
		encoded, err := json.Marshal(content)
		Expect(err).NotTo(HaveOccurred())
		Expect(database.Model(&storage.Document{}).Where("id = ?", document.ID).Update("content", storage.JSON(encoded)).Error).To(Succeed())
		legacy, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		result, err := legacy.Effects(ctx, query.EffectsOptions{Selector: "func:Add", Scope: query.ModuleScopeOptions{RootKey: effectsModule, Location: checkout}})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Deterministic).To(BeFalse())
		Expect(result.Effects).To(Equal([]query.SymbolEffect{{
			Effect: effects.Unknown, Path: []string{effectsModule + ".Add"}, Evidence: effectsModule + ".Add has no recorded effects; reindex it",
		}}))
	})
})
