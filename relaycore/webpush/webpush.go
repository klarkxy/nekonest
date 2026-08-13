package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/klarkxy/nekonest/relaycore/internal/opslog"
)

// Subscription is the browser push endpoint + keys.
type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}

type notificationPayload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	DeviceID  string `json:"device_id"`
	SessionID string `json:"session_id"`
	Tag       string `json:"tag"`
}

var (
	vapidOnce sync.Once
	vapidPub  string
	vapidPriv string
	vapidSub  string

	httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					host = addr
					port = "443"
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				var last error
				d := net.Dialer{Timeout: 5 * time.Second}
				for _, ipa := range ips {
					if err := rejectIP(ipa.IP); err != nil {
						last = err
						continue
					}
					c, err := d.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
					if err == nil {
						return c, nil
					}
					last = err
				}
				if last == nil {
					last = errInvalidEndpoint
				}
				return nil, last
			},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return http.ErrUseLastResponse
			}
			return validatePushURL(req.URL.String())
		},
	}

	pushWorkersOnce sync.Once
	pushQueue       = make(chan notificationJob, 128)
)

const pushWorkerCount = 2

type notificationJob struct {
	subscriptions []Subscription
	payload       []byte
	onGone        func(endpoint string)
}

func loadVAPID() {
	vapidOnce.Do(func() {
		vapidPub = os.Getenv("NEKONEST_VAPID_PUBLIC_KEY")
		vapidPriv = os.Getenv("NEKONEST_VAPID_PRIVATE_KEY")
		vapidSub = os.Getenv("NEKONEST_VAPID_SUBJECT")
		if vapidSub == "" {
			vapidSub = "mailto:admin@localhost"
		}
	})
}

func Enabled() bool {
	loadVAPID()
	return vapidPub != "" && vapidPriv != ""
}

func PublicKey() string {
	loadVAPID()
	return vapidPub
}

func ValidateEndpoint(raw string) error {
	return validatePushURL(raw)
}

// ValidateKeys checks the Web Push ECDH public key and auth secret before they
// are persisted or passed to the crypto library.
func ValidateKeys(p256dh, auth string) error {
	publicKey, err := decodeBase64URL(p256dh)
	if err != nil || len(publicKey) != 65 || publicKey[0] != 0x04 {
		return errInvalidPushKeys
	}
	authSecret, err := decodeBase64URL(auth)
	if err != nil || len(authSecret) != 16 {
		return errInvalidPushKeys
	}
	return nil
}

func decodeBase64URL(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func validatePushURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errInvalidEndpoint
	}
	if strings.ToLower(u.Scheme) != "https" {
		return errInvalidEndpoint
	}
	host := u.Hostname()
	if host == "" {
		return errInvalidEndpoint
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") || host == "metadata.google.internal" {
		return errInvalidEndpoint
	}
	if ip := net.ParseIP(host); ip != nil {
		return rejectIP(ip)
	}
	// Hostname: DialContext re-checks resolved IPs (blocks DNS rebinding to RFC1918).
	return nil
}

func rejectIP(ip net.IP) error {
	if ip == nil {
		return errInvalidEndpoint
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return errInvalidEndpoint
	}
	// 169.254.169.254 metadata etc. covered by link-local; also block CGNAT 100.64/10
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return errInvalidEndpoint
		}
	}
	return nil
}

var errInvalidEndpoint = &pushError{"invalid push endpoint"}
var errInvalidPushKeys = &pushError{"invalid push subscription keys"}

type pushError struct{ s string }

func (e *pushError) Error() string { return e.s }

// Send places a Web Push delivery on a bounded worker queue. It returns false
// when delivery is disabled, invalid, or the queue is saturated.
func Send(
	subs []Subscription,
	title, body, openURL, deviceID, sessionID string,
	onGone func(endpoint string),
) bool {
	loadVAPID()
	if len(subs) == 0 {
		return false
	}
	valid := make([]Subscription, 0, len(subs))
	for _, s := range subs {
		if err := validatePushURL(s.Endpoint); err != nil {
			opslog.Warn("server.push", "subscription_endpoint_rejected", "push subscription endpoint rejected")
			continue
		}
		if err := ValidateKeys(s.P256DH, s.Auth); err != nil {
			opslog.Warn("server.push", "subscription_keys_rejected", "push subscription keys rejected")
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return false
	}
	if !Enabled() {
		opslog.Info("server.push", "delivery_disabled", "push delivery skipped because VAPID is not configured", "subscription_count", len(valid))
		return false
	}
	payload, _ := marshalNotification(title, body, openURL, deviceID, sessionID)
	startPushWorkers()
	job := notificationJob{
		subscriptions: valid,
		payload:       payload,
		onGone:        onGone,
	}
	if !enqueueNotification(pushQueue, job) {
		opslog.Warn("server.push", "queue_full", "push notification dropped because queue is full", "subscription_count", len(valid))
		return false
	}
	return true
}

func marshalNotification(title, body, openURL, deviceID, sessionID string) ([]byte, error) {
	return json.Marshal(notificationPayload{
		Title:     title,
		Body:      body,
		URL:       openURL,
		DeviceID:  deviceID,
		SessionID: sessionID,
		Tag:       "nekonest:" + deviceID + ":" + sessionID,
	})
}

func startPushWorkers() {
	pushWorkersOnce.Do(func() {
		for range pushWorkerCount {
			go pushWorker(pushQueue)
		}
	})
}

func enqueueNotification(queue chan notificationJob, job notificationJob) bool {
	select {
	case queue <- job:
		return true
	default:
		return false
	}
}

func pushWorker(queue <-chan notificationJob) {
	for job := range queue {
		deliverNotification(job)
	}
}

func deliverNotification(job notificationJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, s := range job.subscriptions {
		select {
		case <-ctx.Done():
			return
		default:
		}
		sub := &webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys: webpush.Keys{
				P256dh: s.P256DH,
				Auth:   s.Auth,
			},
		}
		resp, err := webpush.SendNotificationWithContext(ctx, job.payload, sub, &webpush.Options{
			Subscriber:      vapidSub,
			VAPIDPublicKey:  vapidPub,
			VAPIDPrivateKey: vapidPriv,
			TTL:             60,
			HTTPClient:      httpClient,
		})
		if err != nil {
			opslog.Error("server.push", "delivery_failed", "push delivery failed", err)
			continue
		}
		if resp == nil {
			continue
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		handleDeliveryStatus(job, s.Endpoint, status)
		if status >= 400 {
			opslog.Warn("server.push", "delivery_http_error", "push endpoint returned an error status", "status", status)
		}
	}
}

func isGoneStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

func handleDeliveryStatus(job notificationJob, endpoint string, status int) {
	if isGoneStatus(status) && job.onGone != nil {
		job.onGone(endpoint)
	}
}
