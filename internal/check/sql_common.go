package check

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"
)

// remainingTimeout ctx bitiş süresine kalan zamanı döner; süre yoksa (veya
// negatifse) sürücülerin bağlanma zaman aşımı için makul bir varsayılan verir.
// Asıl iptal her zaman ctx üzerinden gelir; bu sadece sürücü tarafındaki dial
// zaman aşımı alanları için bir güvenlik payıdır.
func remainingTimeout(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

// sqlTarget kullanıcı adı ve veritabanı adını (şifre olmadan) hedefe ekler.
func sqlTarget(user, host string, port int, database string) string {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	target := addr
	if user != "" {
		target = user + "@" + addr
	}
	if database != "" {
		target += "/" + database
	}
	return target
}

// runSQLQuery sorguyu çalıştırır, satırları sayar ve istenirse ilk satırın
// ilk sütununu beklenen değerle karşılaştırır. MySQL/PostgreSQL/MSSQL
// kontrolleri tarafından ortak kullanılır.
func runSQLQuery(ctx context.Context, dg *diagRun, db *sql.DB, start time.Time, query, expected string) Result {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		dg.fail(ctx, PhaseQuery, err)
		return down("Sorgu çalıştırılamadı: " + describeErr(ctx, err))
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		dg.fail(ctx, PhaseQuery, err)
		return down("Sorgu sonucu okunamadı: " + err.Error())
	}

	count := 0
	var first string
	haveFirst := false
	for rows.Next() {
		if !haveFirst {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				dg.fail(ctx, PhaseQuery, err)
				return down("Sorgu sonucu okunamadı: " + err.Error())
			}
			if len(vals) > 0 {
				first = sqlValueString(vals[0])
			}
			haveFirst = true
		}
		count++
	}
	if err := rows.Err(); err != nil {
		dg.fail(ctx, PhaseQuery, err)
		return down("Sorgu sonucu okunamadı: " + describeErr(ctx, err))
	}

	if expected != "" {
		if !haveFirst {
			dg.failClass(PhaseResponse, ClassMismatch)
			return down(fmt.Sprintf("Sorgu sonuç döndürmedi (beklenen: %q)", expected))
		}
		if first != expected {
			dg.failClass(PhaseResponse, ClassMismatch)
			return down(fmt.Sprintf("Beklenmeyen sonuç: %q (beklenen: %q)", truncate(first, 80), expected))
		}
	}
	return Result{Up: true, PingMs: msSince(start), Message: fmt.Sprintf("Sorgu başarılı (%d satır)", count)}
}

// sqlValueString database/sql'in driver.Value türlerini karşılaştırma için
// metne çevirir.
func sqlValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return fmt.Sprint(t)
	}
}
