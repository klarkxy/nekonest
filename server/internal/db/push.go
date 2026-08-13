package db

import (
	"time"

	corestore "github.com/klarkxy/nekonest/relaycore/store"
)

const maxPushSubscriptionsPerDevice = 32

// PushSubscription represents a Web Push subscription.
type PushSubscription = corestore.PushSubscription

// SavePushSubscription stores one endpoint mapping per device. A browser reuses
// the same endpoint while the user subscribes to multiple devices.
func (db *DB) SavePushSubscription(sub *PushSubscription) error {
	now := time.Now().Unix()
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`
		INSERT INTO push_subscriptions (device_id, phone_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint, device_id) DO UPDATE SET
			phone_id = excluded.phone_id,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			created_at = excluded.created_at`,
		sub.DeviceID, sub.PhoneID, sub.Endpoint, sub.P256DH, sub.Auth, now,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(`
		DELETE FROM push_subscriptions
		WHERE id IN (
			SELECT id FROM push_subscriptions
			WHERE device_id = ?
			ORDER BY created_at DESC, id DESC
			LIMIT -1 OFFSET ?
		)`,
		sub.DeviceID, maxPushSubscriptionsPerDevice,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// GetPushSubscriptions returns all subscriptions for a device.
func (db *DB) GetPushSubscriptions(deviceID string) ([]*PushSubscription, error) {
	rows, err := db.conn.Query(
		`SELECT id, device_id, phone_id, endpoint, p256dh, auth FROM push_subscriptions WHERE device_id = ?`,
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*PushSubscription
	for rows.Next() {
		sub := &PushSubscription{}
		if err := rows.Scan(&sub.ID, &sub.DeviceID, &sub.PhoneID, &sub.Endpoint, &sub.P256DH, &sub.Auth); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// DeletePushSubscription removes a subscription by endpoint.
func (db *DB) DeletePushSubscription(endpoint string) error {
	_, err := db.conn.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}
