schema "dbo" {}

table "AcmeOrders" {
  schema = schema.dbo
  column "OrderGUID" {
    type = uniqueidentifier
  }
  column "CustomerGUID" {
    type = varchar(36)
  }
  column "Total" {
    type = decimal(18, 2)
  }
  primary_key {
    columns = [column.OrderGUID]
  }
  index "IX_AcmeOrders_Customer" {
    columns = [column.CustomerGUID]
  }
}
