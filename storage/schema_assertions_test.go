package storage_test

import (
	"reflect"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

type tableSchema struct {
	model       any
	indexes     []string
	constraints []string
}

// moduleSchema is the base schema and symbol index from docs/symbol-index-storage.md: every model with
// the named indexes of the index inventory and the named foreign keys and checks of the HCL.
var moduleSchema = []tableSchema{
	{model: &storage.ModuleRoot{}, indexes: []string{"modules_root_key_key"}, constraints: []string{"modules_root_key_check"}},
	{model: &storage.ModuleLocation{},
		indexes:     []string{"locations_root_id_id_key", "locations_root_id_canonical_path_key", "locations_parent_idx"},
		constraints: []string{"locations_root_id_fkey", "locations_parent_location_id_fkey", "locations_canonical_path_check", "locations_kind_check"}},
	{model: &storage.ModuleSnapshot{},
		indexes: []string{"snapshots_root_id_id_key", "snapshots_location_id_id_key", "snapshots_location_started_idx", "snapshots_base_idx", "snapshots_root_revision_idx"},
		constraints: []string{"snapshots_root_id_fkey", "snapshots_location_fkey", "snapshots_base_fkey", "snapshots_worktree_state_check",
			"snapshots_revision_check", "snapshots_context_hash_check", "snapshots_coverage_check", "snapshots_package_count_check"}},
	{model: &storage.ModuleLocationHead{}, constraints: []string{"location_heads_location_fkey", "location_heads_snapshot_fkey", "location_heads_version_check"}},
	{model: &storage.ModulePrimary{}, constraints: []string{"primary_locations_location_fkey"}},
	{model: &storage.SourceRevision{},
		indexes:     []string{"source_revisions_root_id_path_key_id_key", "source_revisions_root_id_path_key_content_hash_package_path_key"},
		constraints: []string{"source_revisions_root_id_fkey", "source_revisions_path_key_check", "source_revisions_content_hash_check", "source_revisions_size_bytes_check"}},
	{model: &storage.SourceDelta{}, indexes: []string{"source_deltas_revision_id_idx"},
		constraints: []string{"source_deltas_snapshot_fkey", "source_deltas_revision_fkey", "source_deltas_operation_check"}},
	{model: &storage.Symbol{}, indexes: []string{"symbols_canonical_key_key", "symbols_lookup_idx", "symbols_search_idx"},
		constraints: []string{"symbols_owner_id_fkey", "symbols_id_check", "symbols_identity_version_check", "symbols_kind_check", "symbols_visibility_check",
			"symbols_name_check", "symbols_canonical_key_check", "symbols_module_key_check"}},
	{model: &storage.Document{}, indexes: []string{"documents_root_id_id_key", "documents_root_id_path_key_input_hash_key", "documents_source_idx"},
		constraints: []string{"documents_root_id_fkey", "documents_source_revision_fkey", "documents_input_hash_check", "documents_path_key_check",
			"documents_indexer_version_check", "documents_coverage_check", "documents_symbol_count_check", "documents_occurrence_count_check", "documents_excluded_check"}},
	{model: &storage.SymbolPosting{}, indexes: []string{"symbol_postings_symbol_idx"},
		constraints: []string{"symbol_postings_document_fkey", "symbol_postings_symbol_id_fkey", "symbol_postings_role_check", "symbol_postings_occurrence_count_check"}},
	{model: &storage.PackageCoverage{}, indexes: []string{"package_coverage_input_idx"},
		constraints: []string{"package_coverage_snapshot_fkey", "package_coverage_input_hash_check", "package_coverage_export_shape_hash_check",
			"package_coverage_coverage_check", "package_coverage_indexed_check", "package_coverage_package_path_check", "package_coverage_file_count_check"}},
}

// assertModuleSchema fails when the migrated tables and the GORM models disagree on a column or its
// nullability (a pointer field is exactly a nullable column), or a named index or constraint is missing.
func assertModuleSchema(database *gorm.DB) {
	GinkgoHelper()
	for _, table := range moduleSchema {
		statement := &gorm.Statement{DB: database}
		Expect(statement.Parse(table.model)).To(Succeed())
		name := statement.Schema.Table
		Expect(database.Migrator().HasTable(table.model)).To(BeTrue(), name)
		expected := map[string]bool{}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" {
				expected[field.DBName] = field.FieldType.Kind() == reflect.Ptr
			}
		}
		columns, err := database.Migrator().ColumnTypes(table.model)
		Expect(err).To(Succeed())
		actual := map[string]bool{}
		for _, column := range columns {
			nullable, known := column.Nullable()
			Expect(known).To(BeTrue(), name+"."+column.Name())
			actual[column.Name()] = nullable
		}
		Expect(actual).To(Equal(expected), name)
		for _, index := range table.indexes {
			Expect(database.Migrator().HasIndex(table.model, index)).To(BeTrue(), name+" index "+index)
		}
		for _, constraint := range table.constraints {
			Expect(database.Migrator().HasConstraint(table.model, constraint)).To(BeTrue(), name+" constraint "+constraint)
		}
	}
	for _, name := range append(append([]string{}, legacyTables...), previousGenerationTables...) {
		Expect(database.Migrator().HasTable(name)).To(BeFalse(), name)
	}
}
