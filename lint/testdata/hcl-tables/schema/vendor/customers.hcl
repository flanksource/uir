schema "dbo" {}

table "AcmeCustomers" {
  schema  = schema.dbo
  partial = true
  column "CustomerGUID" {
    type = uniqueidentifier
  }
  column "Name" {
    type = nvarchar(100)
  }
}
