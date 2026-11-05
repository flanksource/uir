package query_test

import (
	"bytes"
	"fmt"
	"os"
	"regexp"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/query/sqlsyntax"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL documentation examples", func() {
	It("parses every fenced SQL example and demonstrates every registered source function", func() {
		functions := map[string]bool{}
		fence := regexp.MustCompile("(?m)^```sql\n([\\s\\S]*?)^```")
		for _, path := range []string{"../docs/sql-query.md", "../docs/sql-examples.md"} {
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			examples := fence.FindAllSubmatchIndex(content, -1)
			Expect(examples).NotTo(BeEmpty(), path)
			for _, example := range examples {
				statement := string(content[example[2]:example[3]])
				location := fmt.Sprintf("%s:%d\n%s", path, bytes.Count(content[:example[2]], []byte("\n"))+1, statement)
				parsed, err := sqlsyntax.Parse(statement)
				Expect(err).NotTo(HaveOccurred(), location)
				collectDocumentedFunctions(parsed.Source, functions)
			}
		}
		for _, function := range query.SQLFunctions {
			Expect(functions).To(HaveKey(function.Name), "missing SQL example for %s", function.Name)
		}
	})
})

func collectDocumentedFunctions(source *sqlsyntax.Source, functions map[string]bool) {
	if source.Name != "" {
		functions[source.Name] = true
	}
	for _, parameter := range source.Parameters {
		if parameter.Source != nil {
			collectDocumentedFunctions(parameter.Source, functions)
		}
	}
}
