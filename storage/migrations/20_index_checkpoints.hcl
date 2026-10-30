table "index_checkpoints" {
  schema = schema.public
  column "key" { type = text }
  column "fingerprint" { type = text }
  column "inputs" { type = jsonb }
  column "results" { type = jsonb }
  column "heads" { type = jsonb }
  primary_key { columns = [column.key] }
}
