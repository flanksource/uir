schema "dbo" {}

table "AcmeCustomers" {
  schema = schema.dbo
  column "CustomerGUID" {
    type = uniqueidentifier
  }
  column "Name" {
    type = nvarchar(100)
  }
}
