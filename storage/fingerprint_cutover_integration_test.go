package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// twoLayerSymbolDeltas is symbol_deltas as the H64b schema declared it before layered fingerprints: one
// shape and one body fingerprint per set.
var twoLayerSymbolDeltas = []string{
	"DROP TABLE symbol_deltas",
	"CREATE TABLE symbol_deltas (snapshot_ordinal bigint NOT NULL, root_ordinal integer NOT NULL, symbol_handle bigint NOT NULL, " +
		"operation text NOT NULL, shape_fp bigint, body_fp bigint, PRIMARY KEY (snapshot_ordinal, symbol_handle))",
}

// downgradeToTwoLayer turns a current database into one the two-layer generation wrote: its symbol
// deltas carry shape_fp and body_fp, and every index table holds one row.
func downgradeToTwoLayer(database *gorm.DB, checkouts preHandleCheckouts) {
	GinkgoHelper()
	for _, statement := range twoLayerSymbolDeltas {
		Expect(database.Exec(statement).Error).To(Succeed(), statement)
	}
	populateH64a(database, checkouts)
}

var _ = Describe("two-layer fingerprint cutover", func() {
	DescribeTable("discards a shape/body-fingerprint index, keeps registrations and task runs, and reindexes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			checkouts := newPreHandleCheckouts()
			legacy := openDB(ctx, config)
			downgradeToTwoLayer(legacy, checkouts)
			Expect(rowCounts(legacy, h64aIndexTables)).To(HaveEach(Equal(int64(1))))

			_, err := storage.OpenReadOnly(ctx, config)
			Expect(err).To(MatchError(ContainSubstring("database predates layered symbol fingerprints; open it read-write once or run uir reindex")))

			cutover := openDB(ctx, config)
			assertModuleSchema(cutover)
			Expect(rowCounts(cutover, h64aIndexTables)).To(HaveEach(BeZero()))
			Expect(rowCounts(cutover, h64aKeptTables)).To(Equal(map[string]int64{"modules": 2, "locations": 3, "primary_locations": 2, "task_runs": 1}))
			Expect(rootOrdinals(cutover)).To(Equal(map[string]int32{"example.org/zeta": 1, legacyModule: 2}), "registered roots keep their ordinals")
			Expect([]bool{cutover.Migrator().HasColumn("symbol_deltas", "shape_fp"), cutover.Migrator().HasColumn("symbol_deltas", "public_fp")}).
				To(Equal([]bool{false, true}))

			Expect(reindex(ctx, cutover, checkouts.primary)).To(And(HaveField("ParsedFiles", 1), HaveField("HeadVersion", int64(1)), HaveField("Unchanged", false)))
			reindexed := rowCounts(cutover, append(append([]string{}, h64aIndexTables...), h64aKeptTables...))
			Expect(reindexed).To(HaveKeyWithValue("symbol_deltas", int64(1)))
			Expect(rowCounts(openDB(ctx, config), append(append([]string{}, h64aIndexTables...), h64aKeptTables...))).To(Equal(reindexed),
				"reopening a layered database discards nothing")
		},
		Entry("SQLite", sqliteOptions("two-layer.db")),
		Entry("PostgreSQL", postgresOptions("uir_two_layer_cutover")),
	)
})
