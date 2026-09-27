package store

// Bu dalda migration 3 (paralel geliştirilen başka bir özellik) yok; migrate()
// numaralarda boşluğa izin vermediği için testlerde boş bir yer tutucu
// kaydedilir. Yalnızca test ikilisinde derlenir, üretimi etkilemez; dallar
// birleştirildiğinde gerçek migration kayıtlı olacağından hiçbir şey yapmaz
// ve silinebilir.
func init() {
	if _, ok := migrations[3]; !ok {
		migrations[3] = "SELECT 1"
	}
}
