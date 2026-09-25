package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

type OrderLine struct {
	VariantID int64
	Qty       int
}

type NewOrder struct {
	UserID  *int64
	Name    string
	Phone   string
	Email   string
	Address string
	Note    string
	ZoneID  int64
	Lines   []OrderLine
}

type Order struct {
	ID          int64
	Number      string
	UserID      *int64 `db:"user_id"`
	Name        string
	Phone       string
	Email       string
	Address     string
	ZoneName    string `db:"zone_name"`
	DeliveryFee int    `db:"delivery_fee"`
	Subtotal    int
	Total       int
	Status      string
	Note        string
	AdminNote   string    `db:"admin_note"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type OrderItem struct {
	ID          int64
	OrderID     int64  `db:"order_id"`
	VariantID   *int64 `db:"variant_id"`
	ProductName string `db:"product_name"`
	VariantName string `db:"variant_name"`
	UnitPrice   int    `db:"unit_price"`
	Qty         int
}

type OrderFull struct {
	Order
	Items []OrderItem
}

type ErrOutOfStock struct {
	VariantID int64
	Name      string
	Available int
}

func (e ErrOutOfStock) Error() string {
	return fmt.Sprintf("only %d of %s available", e.Available, e.Name)
}

var transitions = map[string][]string{
	"new":       {"confirmed", "cancelled"},
	"confirmed": {"shipped", "cancelled"},
	"shipped":   {"delivered", "cancelled"},
}

func CanTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// NextStatuses lists the statuses an order may move to from its current one.
func NextStatuses(from string) []string { return slices.Clone(transitions[from]) }

const orderCols = `id, number, user_id, name, phone, email, address, zone_name, delivery_fee, subtotal, total, status, note, admin_note, created_at, updated_at`
const itemCols = `id, order_id, variant_id, product_name, variant_name, unit_price, qty`

// PlaceOrder locks each variant row, verifies stock, decrements it, and
// inserts the order with snapshotted names and prices, all in one transaction.
func (s *Store) PlaceOrder(ctx context.Context, in NewOrder) (OrderFull, error) {
	var out OrderFull

	// Normalize lines: reject qty <= 0, merge duplicates by VariantID, sort by VariantID.
	lineMap := make(map[int64]int)
	for _, l := range in.Lines {
		if l.Qty <= 0 {
			return out, fmt.Errorf("line qty must be > 0")
		}
		lineMap[l.VariantID] += l.Qty
	}
	var lines []OrderLine
	for vid, qty := range lineMap {
		lines = append(lines, OrderLine{VariantID: vid, Qty: qty})
	}
	// Sort by VariantID to prevent deadlocks.
	for i := 0; i < len(lines)-1; i++ {
		for j := i + 1; j < len(lines); j++ {
			if lines[j].VariantID < lines[i].VariantID {
				lines[i], lines[j] = lines[j], lines[i]
			}
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)

	var zoneName string
	var fee int
	if err := tx.QueryRow(ctx, `select name, fee from delivery_zones where id = $1 and active`, in.ZoneID).Scan(&zoneName, &fee); err != nil {
		return out, mapErr(err)
	}
	type snap struct {
		productName, variantName string
		price                    int
	}
	snaps := make([]snap, len(lines))
	subtotal := 0
	for i, l := range lines {
		var sn snap
		var stock int
		var active bool
		err := tx.QueryRow(ctx, `select p.name, v.name, v.price, v.stock, p.active from variants v join products p on p.id = v.product_id where v.id = $1 for update of v`, l.VariantID).
			Scan(&sn.productName, &sn.variantName, &sn.price, &stock, &active)
		if err != nil {
			return out, mapErr(err)
		}
		if !active {
			return out, ErrNotFound
		}
		if stock < l.Qty {
			return out, ErrOutOfStock{VariantID: l.VariantID, Name: sn.productName + " " + sn.variantName, Available: stock}
		}
		if _, err := tx.Exec(ctx, `update variants set stock = stock - $2 where id = $1`, l.VariantID, l.Qty); err != nil {
			return out, err
		}
		snaps[i] = sn
		subtotal += sn.price * l.Qty
	}
	rows, _ := tx.Query(ctx, `insert into orders (number, user_id, name, phone, email, address, zone_name, delivery_fee, subtotal, total, note)
		values ('AV-' || lpad(nextval('order_number_seq')::text, 6, '0'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning `+orderCols,
		in.UserID, in.Name, in.Phone, in.Email, in.Address, zoneName, fee, subtotal, subtotal+fee, in.Note)
	out.Order, err = pgx.CollectOneRow(rows, pgx.RowToStructByName[Order])
	if err != nil {
		return out, err
	}
	for i, l := range lines {
		irows, _ := tx.Query(ctx, `insert into order_items (order_id, variant_id, product_name, variant_name, unit_price, qty) values ($1, $2, $3, $4, $5, $6) returning `+itemCols,
			out.ID, l.VariantID, snaps[i].productName, snaps[i].variantName, snaps[i].price, l.Qty)
		item, err := pgx.CollectOneRow(irows, pgx.RowToStructByName[OrderItem])
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) getOrderWhere(ctx context.Context, where string, arg any) (OrderFull, error) {
	var out OrderFull
	rows, _ := s.db.Query(ctx, `select `+orderCols+` from orders where `+where, arg)
	o, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Order])
	if err != nil {
		return out, mapErr(err)
	}
	out.Order = o
	irows, err := s.db.Query(ctx, `select `+itemCols+` from order_items where order_id = $1 order by id`, o.ID)
	if err != nil {
		return out, err
	}
	out.Items, err = pgx.CollectRows(irows, pgx.RowToStructByName[OrderItem])
	return out, err
}

func (s *Store) GetOrderByNumber(ctx context.Context, number string) (OrderFull, error) {
	return s.getOrderWhere(ctx, "number = $1", number)
}

func (s *Store) GetOrder(ctx context.Context, id int64) (OrderFull, error) {
	return s.getOrderWhere(ctx, "id = $1", id)
}

func (s *Store) ListOrders(ctx context.Context, status string, limit int) ([]Order, error) {
	rows, err := s.db.Query(ctx, `select `+orderCols+` from orders where ($1 = '' or status = $1) order by created_at desc, id desc limit $2`, status, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Order])
}

func (s *Store) ListOrdersByUser(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := s.db.Query(ctx, `select `+orderCols+` from orders where user_id = $1 order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Order])
}

// UpdateOrderStatus enforces the transition table and restores stock when cancelling.
func (s *Store) UpdateOrderStatus(ctx context.Context, id int64, to string) (OrderFull, error) {
	var out OrderFull
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var from string
	if err := tx.QueryRow(ctx, `select status from orders where id = $1 for update`, id).Scan(&from); err != nil {
		return out, mapErr(err)
	}
	if !CanTransition(from, to) {
		return out, ErrTransition
	}
	if to == "cancelled" {
		// Lock affected variants in id order to prevent deadlocks.
		lockRows, err := tx.Query(ctx, `select id from variants where id in (select variant_id from order_items where order_id = $1) order by id for update`, id)
		if err != nil {
			return out, err
		}
		_, err = pgx.CollectRows(lockRows, pgx.RowToStructByName[struct{ ID int64 }])
		if err != nil {
			return out, err
		}
		// Restore stock from summed per-variant qty.
		if _, err := tx.Exec(ctx, `update variants v set stock = v.stock + oi.qty from (select variant_id, sum(qty) as qty from order_items where order_id = $1 and variant_id is not null group by variant_id) oi where v.id = oi.variant_id`, id); err != nil {
			return out, err
		}
	}
	if _, err := tx.Exec(ctx, `update orders set status = $2, updated_at = clock_timestamp() where id = $1`, id, to); err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.GetOrder(ctx, id)
}

func (s *Store) SetAdminNote(ctx context.Context, id int64, note string) error {
	tag, err := s.db.Exec(ctx, `update orders set admin_note = $2, updated_at = clock_timestamp() where id = $1`, id, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type LowStock struct {
	ProductName string `db:"product_name"`
	VariantName string `db:"variant_name"`
	Stock       int
}

type Dashboard struct {
	NewOrders int
	LowStock  []LowStock
	Recent    []Order
}

func (s *Store) Dashboard(ctx context.Context) (Dashboard, error) {
	var d Dashboard
	if err := s.db.QueryRow(ctx, `select count(*) from orders where status = 'new'`).Scan(&d.NewOrders); err != nil {
		return d, err
	}
	rows, err := s.db.Query(ctx, `select p.name as product_name, v.name as variant_name, v.stock from variants v join products p on p.id = v.product_id where v.stock <= 5 and p.active order by v.stock, p.name`)
	if err != nil {
		return d, err
	}
	if d.LowStock, err = pgx.CollectRows(rows, pgx.RowToStructByName[LowStock]); err != nil {
		return d, err
	}
	d.Recent, err = s.ListOrders(ctx, "", 10)
	return d, err
}
