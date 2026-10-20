package security

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiterBucket(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	l := NewLimiter(1, 3) // 1 в секунду, всплеск 3
	l.now = func() time.Time { return now }
	for range 3 {
		ok, _ := l.Allow("a")
		require.True(t, ok)
	}
	ok, wait := l.Allow("a")
	require.False(t, ok)
	require.Equal(t, time.Second, wait)
	ok, _ = l.Allow("b")
	require.True(t, ok, "у другого ключа своя корзина")

	now = now.Add(1500 * time.Millisecond)
	ok, _ = l.Allow("a")
	require.True(t, ok, "за полторы секунды набежал один токен")
	ok, wait = l.Allow("a")
	require.False(t, ok)
	require.Equal(t, 500*time.Millisecond, wait)
}

func TestLimiterForgetsIdleKeys(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	l := NewLimiter(1, 3)
	l.now = func() time.Time { return now }
	for i := range 100 {
		l.Allow(string(rune('a'+i%26)) + string(rune(i)))
	}
	now = now.Add(time.Hour)
	l.Allow("x")
	require.LessOrEqual(t, len(l.buckets), 1, "полные корзины выброшены — память не растёт")
}

func TestLimiterDisabled(t *testing.T) {
	var l *Limiter // nil — без ограничения
	ok, _ := l.Allow("a")
	require.True(t, ok)
	require.Nil(t, NewLimiter(0, 10))
}

func TestClientIPTrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("127.0.0.1/32")}
	cases := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"203.0.113.5:4000", []string{"1.2.3.4"}, "203.0.113.5"},                        // не доверенный — заголовок не в счёт
		{"127.0.0.1:4000", nil, "127.0.0.1"},                                            // прокси без заголовка
		{"127.0.0.1:4000", []string{"198.51.100.7"}, "198.51.100.7"},                    // клиент за прокси
		{"127.0.0.1:4000", []string{"6.6.6.6, 198.51.100.7, 10.1.2.3"}, "198.51.100.7"}, // левый подставлен клиентом
		{"127.0.0.1:4000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},         // несколько заголовков
		{"127.0.0.1:4000", []string{"мусор"}, "127.0.0.1"},                              // негодный адрес — адрес соединения
		{"127.0.0.1:4000", []string{"10.9.9.9, 10.1.2.3"}, "10.9.9.9"},                  // вся цепочка доверенная
		{"[::ffff:127.0.0.1]:4000", []string{"198.51.100.7"}, "198.51.100.7"},           // IPv4 в IPv6
	}
	for _, c := range cases {
		require.Equal(t, c.want, ClientIP(c.remote, c.xff, trusted), "%v", c)
	}
	require.Equal(t, "203.0.113.5", ClientIP("203.0.113.5:4000", []string{"1.2.3.4"}, nil), "без доверенных прокси")
}

func TestRateKey(t *testing.T) {
	require.Equal(t, "2001:db8:1:2::/64", RateKey("2001:db8:1:2:aaaa::1"))
	require.Equal(t, RateKey("2001:db8:1:2:aaaa::1"), RateKey("2001:db8:1:2:bbbb:cccc:dddd:eeee"))
	require.NotEqual(t, RateKey("2001:db8:1:2::1"), RateKey("2001:db8:1:3::1"))
	require.Equal(t, "203.0.113.5", RateKey("203.0.113.5"))
	require.Equal(t, "не адрес", RateKey("не адрес"))
}
