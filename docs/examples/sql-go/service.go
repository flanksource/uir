package service

type Order struct {
	Total int
}

type Repository interface {
	Save(Order) error
}

type Store struct {
	orders []Order
}

type AuditedStore struct {
	Store
}

var LastTotal int

func (store *Store) Save(order Order) error {
	store.orders = append(store.orders, order)
	LastTotal = order.Total
	return nil
}

func Run() error {
	store := &Store{}
	return store.Save(Order{Total: 100})
}

func Entry() error {
	return Run()
}

func Replay() error {
	return Run()
}

func Load() int {
	return LastTotal
}

func Process(repository Repository, order Order) error {
	return repository.Save(order)
}
