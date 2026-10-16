// properties holds the global runtime settings commons-db's context reads (dbcontext.Properties) and
// writes (dbcontext.UpdateProperty) by name; the mounted connection service consults it, so the table
// mirrors commons-db's query schema rather than a uir-specific shape.
table "properties" {
  schema = schema.public

  column "name" {
    type = text
    null = false
  }
  column "value" {
    type = text
    null = false
  }
  column "created_by" {
    type = uuid
    null = true
  }
  column "created_at" {
    type    = timestamptz
    null    = true
    default = sql("CURRENT_TIMESTAMP")
  }
  column "updated_at" {
    type    = timestamptz
    null    = true
    default = sql("CURRENT_TIMESTAMP")
  }
  column "deleted_at" {
    type = timestamptz
    null = true
  }

  primary_key { columns = [column.name] }
}
