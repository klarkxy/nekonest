package db

import "time"

// PushSubscription represents a Web Push subscription.
type PushSubscription struct {
	ID       int64  `json:"id"`
	DeviceID string `json:"device_id"`
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// SavePushSubscription stores a push subscription.
func (db *DB) SavePushSubscription(sub *PushSubscription) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(`
		INSERT INTO push_subscriptions (device_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET device_id = ?, p256dh = ?, auth = ?`,
		sub.DeviceID, sub.Endpoint, sub.P256DH, sub.Auth, now,
		sub.DeviceID, sub.P256DH, sub.Auth,
	)
	return err
}

// GetPushSubscriptions returns all subscriptions for a device.
func (db *DB) GetPushSubscriptions(deviceID string) ([]*PushSubscription, error) {
	rows, err := db.conn.Query(
		`SELECT id, device_id, endpoint, p256dh, auth FROM push_subscriptions WHERE device_id = ?`,
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*PushSubscription
	for rows.Next() {
		sub := &PushSubscription{}
		if err := rows.Scan(&sub.ID, &sub.DeviceID, &sub.Endpoint, &sub.P256DH, &sub.Auth); err != nil {
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
