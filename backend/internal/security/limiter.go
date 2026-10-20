package security

import (
	"math"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Limiter — корзины токенов по ключу (пользователь, IP) в памяти процесса.
// api один на развёртывание (дополнение §18), общий лимитер между
// процессами не нужен; после перезапуска корзины снова полные.
//
// nil — без ограничения: так выключенный в настройках лимит не требует
// проверок у вызывающего.
type Limiter struct {
	rate  float64 // токенов в секунду
	burst float64

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter — rate токенов в секунду, не больше burst в запасе. Нулевая
// частота или всплеск — лимит выключен (nil).
func NewLimiter(rate float64, burst int) *Limiter {
	if rate <= 0 || burst <= 0 {
		return nil
	}
	return &Limiter{rate: rate, burst: float64(burst), buckets: map[string]*bucket{}, now: time.Now}
}

// PerHour — n событий в час со всплеском n.
func PerHour(n int) *Limiter { return NewLimiter(float64(n)/3600, n) }

// Allow тратит токен ключа. Нет токена — false и время до следующего.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = l.refill(b, now)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration(math.Ceil((1 - b.tokens) / l.rate * float64(time.Second)))
}

func (l *Limiter) refill(b *bucket, now time.Time) float64 {
	return min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
}

// sweep выбрасывает полные корзины: полная корзина ничем не отличается
// от новой, а без чистки карта растёт с каждым новым адресом. Обход — не
// чаще раза в минуту, чтобы не платить за него на каждом запросе.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if l.refill(b, now) >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// ClientIP — адрес клиента для лимитов и журнала попыток входа.
//
// X-Forwarded-For учитывается, только если соединение пришло от
// доверенного прокси: иначе клиент подставит любой адрес и обойдёт лимит.
// Цепочка читается справа: каждый доверенный прокси дописывает адрес
// своего собеседника, первый справа недоверенный — клиент; левее клиент
// пишет что хочет (дополнение §18).
func ClientIP(remoteAddr string, forwardedFor []string, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !isTrusted(peer, trusted) {
		return peer.String()
	}
	var hops []string
	for _, h := range forwardedFor {
		hops = append(hops, strings.Split(h, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// Негодную запись дописал не доверенный прокси — дальше
			// цепочке верить нельзя.
			break
		}
		client = addr.Unmap()
		if !isTrusted(client, trusted) {
			break
		}
	}
	return client.String()
}

// RateKey — ключ лимита по адресу клиента. IPv6 сводится к сети /64:
// провайдер выдаёт клиенту целую сеть, и по одному адресу лимит обходился
// бы перебором адресов. Негодная строка возвращается как есть.
func RateKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Unmap().Is4() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
