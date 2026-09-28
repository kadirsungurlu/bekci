// Ayarlar › Kontrol noktaları (Probes.svelte).
export default {
  title: 'Kontrol noktaları',
  /** {probe}: <code>probe</code>; {locations}: kalın "Konumlar" (locations). */
  intro:
    'Monitörlerinizi farklı şehir veya ağlardan da kontrol edin. Kontrol noktası, bu uygulamanın başka bir sunucuda {probe} modunda çalışan bir kopyasıdır; kendisine atanan monitörleri kontrol edip sonuçları buraya bildirir. Monitör formundaki {locations} bölümünden atanır.',
  locations: 'Konumlar',
  newProbe: 'Yeni kontrol noktası',
  empty: 'Henüz kontrol noktası yok. Tüm kontroller şu an yalnızca bu sunucudan yapılıyor.',
  state: {
    disabled: 'Devre dışı',
    online: 'Çevrimiçi',
    offline: 'Çevrimdışı',
  },
  col: {
    status: 'Durum',
    lastSeen: 'Son görülme',
    address: 'Adres',
    monitors: 'Monitör',
  },
  neverConnected: 'Hiç bağlanmadı',
  lockedTo: '{ip} IP’sine kilitli',
  lockPending: 'IP kilidi açık; ilk bağlantıda sabitlenir',
  actionsFor: '{name} için işlemler',
  /** {cmd}: güncelleme komutu (<code>). */
  foot: 'Son 90 saniyede sonuç gönderen kontrol noktası çevrimiçi sayılır. Güncellemek için sunucuda: {cmd}, sonra kurulum komutunu tekrar çalıştırın (komut, satır menüsündeki “Token’ı yenile” ile yeniden alınır).',
  menu: {
    enable: 'Etkinleştir',
    disable: 'Devre dışı bırak',
    regenerate: 'Token’ı yenile',
  },
  confirm: {
    regenerateTitle: 'Token’ı yenile',
    regenerateMessage: '“{name}” — Eski token hemen geçersiz olur; kontrol noktasını yeni token ile yeniden başlatın.',
    regenerateConfirm: 'Token’ı yenile',
    disableTitle: 'Devre dışı bırak',
    disableMessage:
      '“{name}” sonuç gönderemeyecek ve konum hesaplarına katılmayacak. Daha sonra yeniden etkinleştirebilirsiniz.',
    disableConfirm: 'Devre dışı bırak',
    deleteTitle: 'Kontrol noktasını sil',
    deleteMessage: '“{name}” — Bu kontrol noktası tüm monitörlerden çıkarılacak.',
  },
  toast: {
    saved: 'Kontrol noktası kaydedildi',
    ipReset: 'IP kilidi sıfırlandı',
    disabled: 'Kontrol noktası devre dışı bırakıldı',
    enabled: 'Kontrol noktası etkinleştirildi',
    deleted: 'Kontrol noktası silindi',
  },
  form: {
    errName: 'Kontrol noktasına bir ad verin (ör. Frankfurt).',
    errNameRequired: 'Ad gerekli.',
    namePlaceholder: 'ör. Frankfurt',
    nameHelp: 'Konumu anlatan kısa bir ad; monitör detayında ve bildirimlerde görünür.',
    editTitle: 'Kontrol noktasını düzenle',
    ipLock: 'IP’ye kilitle',
    ipLockHelp: 'Kontrol noktası yalnızca ilk bağlandığı IP’den sonuç gönderebilir; sunucu taşınırsa kilidi sıfırlayın.',
    /** {ip}: kalın kilitli IP. */
    lockedIp: 'Kilitli IP: {ip}',
    resetLock: 'Kilidi sıfırla',
  },
  setup: {
    addedTitle: '“{name}” eklendi',
    newTokenTitle: '“{name}” için yeni token',
    shownOnce: 'Bu token yalnızca bir kez gösterilir; kopyalayıp saklayın.',
    token: 'Token',
    command: 'Kurulum komutu',
    /** {root}: kalın "root"; {sudo}: <code>sudo sh <<'UPTIME_KURULUM'</code>. */
    commandHelp: 'Kontrol noktası olacak sunucuda komutun tamamını {root} olarak yapıştırın (ya da ilk satırı {sudo} yapın).',
    /** {path}: <code>/etc/uptime-probe.env</code>; {cmd}: güncelleme komutu. */
    commandAfter: 'Token sunucuda {path} dosyasında (600 izinle) tutulur. Güncellemek için: {cmd}, sonra komutu tekrar çalıştırın.',
    /** {url}: <code> sunucu adresi; {online}: kalın "Çevrimiçi". */
    serverUrl:
      'Sunucu adresi: {url}. Kontrol noktası bu adrese dışarıdan erişebilmeli; birkaç saniye içinde listede {online} görünür.',
    copiedClose: 'Kopyaladım, kapat',
  },
};
