package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Защита от SSRF. Адрес OpenAI-совместимого сервиса задаёт пользователь, а
// запросы по нему делает сервер: без проверки любой пользователь заставил
// бы сервер обращаться к внутренней сети или к метаданным облака.
//
// Проверка двухступенчатая: ValidateBaseURL — при сохранении конфигурации
// (адрес по виду), GuardedHTTPClient — при каждом соединении (фактический
// IP). Только вторая закрывает DNS-rebinding: имя при проверке указывало
// наружу, а при запросе — внутрь.

var (
	errBlockedAddress = errors.New("адрес назначения запрещён")
	cgnat             = netip.MustParsePrefix("100.64.0.0/10")
	metadata          = netip.MustParsePrefix("169.254.0.0/16")
)

// blocked — адрес во внутренней сети. Метаданные облака (link-local)
// запрещены всегда, остальные частные адреса — если администратор их не
// разрешил (локальный vLLM, LM Studio).
func blocked(addr netip.Addr, allowPrivate bool) bool {
	addr = addr.Unmap()
	if metadata.Contains(addr) || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return true
	}
	if addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	if allowPrivate {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || cgnat.Contains(addr)
}

// ValidateBaseURL проверяет адрес API при сохранении конфигурации.
func ValidateBaseURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("%w: нужен адрес вида https://host/path", ErrBadRequest)
	}
	if u.User != nil {
		// Учётные данные в адресе хранились бы открытым текстом.
		return fmt.Errorf("%w: адрес не должен содержать логин и пароль — ключ задаётся отдельно", ErrBadRequest)
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if !allowPrivate {
			return fmt.Errorf("%w: %s", errBlockedAddress, host)
		}
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil && blocked(addr, allowPrivate) {
		return fmt.Errorf("%w: %s", errBlockedAddress, host)
	}
	return nil
}

// GuardedHTTPClient — HTTP-клиент, который проверяет фактический IP при
// каждом соединении, включая перенаправления.
func GuardedHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("%w: %s", errBlockedAddress, host)
			}
			if blocked(addr, allowPrivate) {
				return fmt.Errorf("%w: %s", errBlockedAddress, addr)
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	transport.Proxy = nil // прокси обошёл бы проверку адреса назначения
	return &http.Client{Transport: transport}
}
