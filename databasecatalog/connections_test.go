package databasecatalog_test

import (
	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/databasecatalog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func value(text string) *string { return &text }

var _ = Describe("connection input", func() {
	It("accepts literal credentials, warns, and masks them on reads", func() {
		connection, err := databasecatalog.ApplyConnection(nil, databasecatalog.ConnectionInput{
			Name: value("Warehouse"), Type: value(models.ConnectionTypePostgres), URL: value("postgres://alice:plain-secret@db.example/warehouse"),
			Password: value("plain-secret"),
		})
		Expect(err).ToNot(HaveOccurred())
		view := databasecatalog.ViewConnection(connection)
		Expect(view.LiteralCredentials).To(BeTrue())
		Expect(view.Warnings).ToNot(BeEmpty())
		Expect(view.Password).To(BeEmpty())
		Expect(view.URL).To(BeEmpty())
	})

	It("preserves omitted credentials and clears explicitly supplied empty values", func() {
		existing := models.Connection{Name: "Warehouse", Type: models.ConnectionTypePostgres, URL: "postgres://db.example/warehouse", Password: "op://vault/item/password"}
		kept, err := databasecatalog.ApplyConnection(&existing, databasecatalog.ConnectionInput{Name: value("Renamed")})
		Expect(err).ToNot(HaveOccurred())
		Expect(kept.Password).To(Equal(existing.Password))
		cleared, err := databasecatalog.ApplyConnection(&kept, databasecatalog.ConnectionInput{Password: value("")})
		Expect(err).ToNot(HaveOccurred())
		Expect(cleared.Password).To(BeEmpty())
	})

	It("shows harmless URL options while masking credential query parameters", func() {
		visible := databasecatalog.ViewConnection(models.Connection{Type: models.ConnectionTypeSQLite, URL: "file:catalog.db?mode=ro"})
		Expect(visible.URL).To(Equal("file:catalog.db?mode=ro"))
		Expect(visible.LiteralCredentials).To(BeFalse())
		masked := databasecatalog.ViewConnection(models.Connection{Type: models.ConnectionTypePostgres, URL: "postgres://db.example/catalog?access_token=plain-secret"})
		Expect(masked.URL).To(BeEmpty())
		Expect(masked.LiteralCredentials).To(BeTrue())
	})

	It("round-trips bucket-scoped S3 settings", func() {
		properties := map[string]string{"region": "us-east-1", "bucket": "source-bucket", "usePathStyle": "true"}
		connection, err := databasecatalog.ApplyConnection(nil, databasecatalog.ConnectionInput{
			Name: value("Object store"), Type: value(models.ConnectionTypeS3), Properties: &properties,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(databasecatalog.ViewConnection(connection).Properties).To(Equal(properties))
	})

	It("rejects an invalid S3 path-style setting at save", func() {
		properties := map[string]string{"usePathStyle": "sometimes"}
		_, err := databasecatalog.ApplyConnection(nil, databasecatalog.ConnectionInput{
			Name: value("Object store"), Type: value(models.ConnectionTypeS3), Properties: &properties,
		})
		Expect(err).To(MatchError(ContainSubstring("usePathStyle must be true or false")))
	})

	It("projects all nine commons-db schemas without Kubernetes-only widgets", func() {
		schemas := databasecatalog.ConnectionSchemas()
		Expect(schemas).To(HaveLen(9))
		s3 := schemas[models.ConnectionTypeS3]
		fields := s3["properties"].(map[string]any)
		Expect(fields).To(HaveKey("password"))
		Expect(fields).NotTo(HaveKey("logging"))
		Expect(fields).NotTo(HaveKey("namespace"))
		properties := fields["properties"].(map[string]any)["properties"].(map[string]any)
		Expect(properties).To(HaveKey("bucket"))
		Expect(properties).To(HaveKey("usePathStyle"))
	})
})
