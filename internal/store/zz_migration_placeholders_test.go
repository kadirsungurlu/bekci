package store

// Bu dalda migration 3 ve 4 (paralel geliştirilen başka özellikler) yok;
// migrate() numaralarda boşluğa izin vermediği için testlerde boş yer
// tutucular kaydedilir. Yalnızca test ikilisinde derlenir, üretimi etkilemez.
// Dallar birleştirildiğinde gerçek migration'lar zaten kayıtlı olacağından
// bu dosya hiçbir şey yapmaz; silinebilir.
func init() {
	for _, v := range []int{3, 4} {
		if _, ok := migrations[v]; !ok {
			migrations[v] = "SELECT 1"
		}
	}
}
