package entity

import "sync"

type Book struct {
	Orders          []*Order
	Transactions    []*Transaction
	IncomingOrders  chan *Order
	ProcessedOrders chan *Order
	WaitGroup       *sync.WaitGroup
}

type OrderQueue []*Order

func NewBook(incomingOrders chan *Order, processedOrders chan *Order, waitGroup *sync.WaitGroup) *Book {
	return &Book{
		Orders:          []*Order{},
		Transactions:    []*Transaction{},
		IncomingOrders:  incomingOrders,
		ProcessedOrders: processedOrders,
		WaitGroup:       waitGroup,
	}
}

func (oq *OrderQueue) Add(order *Order) {
	*oq = append(*oq, order)
}

func (oq *OrderQueue) GetNextOrder() *Order {
	if len(*oq) == 0 {
		return nil
	}
	order := (*oq)[0]
	*oq = (*oq)[1:]
	return order
}

func (b *Book) Trade() {
	buyOrders := make(map[string]*OrderQueue)
	sellOrders := make(map[string]*OrderQueue)

	for order := range b.IncomingOrders {
		asset := order.Asset.ID

		if buyOrders[asset] == nil {
			buyOrders[asset] = &OrderQueue{}
		}
		if sellOrders[asset] == nil {
			sellOrders[asset] = &OrderQueue{}
		}

		if order.OrderType == "BUY" {
			b.TryMatch(order, sellOrders[asset], buyOrders[asset])
		} else {
			b.TryMatch(order, buyOrders[asset], sellOrders[asset])
		}
	}
}

func (b *Book) TryMatch(newOrder *Order, availableOrders, pendingOrders *OrderQueue) {
	for {
		potentialMatch := availableOrders.GetNextOrder()

		if potentialMatch == nil {
			break
		}

		if !b.PricesMatch(newOrder, potentialMatch) {
			availableOrders.Add(potentialMatch)
			break
		}

		if potentialMatch.PendingShares > 0 {
			matchedTransaction := b.CreateTransaction(newOrder, potentialMatch)
			b.ProcessTransaction(matchedTransaction)

			if potentialMatch.PendingShares > 0 {
				availableOrders.Add(potentialMatch)
			}

			if newOrder.PendingShares == 0 {
				break
			}
		}
	}

	if newOrder.PendingShares > 0 {
		pendingOrders.Add(newOrder)
	}
}

func (b *Book) PricesMatch(order, matchOrder *Order) bool {
	if order.OrderType == "BUY" {
		return matchOrder.Price <= order.Price
	}
	return matchOrder.Price >= order.Price
}

func (b *Book) CreateTransaction(incomingOrder, matchedOrder *Order) *Transaction {
	var buyOrder, sellOrder *Order

	if incomingOrder.OrderType == "BUY" {
		buyOrder, sellOrder = incomingOrder, matchedOrder
	} else {
		buyOrder, sellOrder = matchedOrder, incomingOrder
	}

	shares := incomingOrder.PendingShares
	if matchedOrder.PendingShares < shares {
		shares = matchedOrder.PendingShares
	}

	return NewTransaction(sellOrder, buyOrder, shares, matchedOrder.Price)
}

func (b *Book) RecordTransaction(transaction *Transaction) {
	b.Transactions = append(b.Transactions, transaction)
	transaction.BuyingOrder.Transactions = append(transaction.BuyingOrder.Transactions, transaction)
	transaction.SellingOrder.Transactions = append(transaction.SellingOrder.Transactions, transaction)
}

func (b *Book) ProcessTransaction(transaction *Transaction) {
	defer b.WaitGroup.Done()

	transaction.Process()
	b.RecordTransaction(transaction)
	b.ProcessedOrders <- transaction.BuyingOrder
	b.ProcessedOrders <- transaction.SellingOrder
}
