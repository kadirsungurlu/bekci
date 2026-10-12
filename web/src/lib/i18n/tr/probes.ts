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
  update: {
    current: 'Güncel',
    currentTitle: 'Ajan panelle aynı sürümde ({panel}).',
    outdated: 'Güncelleme var',
    outdatedAuto: 'Ajan {v}, panel {panel}. Otomatik güncelleme açık: ajan birkaç saniye içinde yeni sürüme geçer.',
    outdatedManual: 'Ajan {v}, panel {panel}. Bu ajanda otomatik güncelleme kapalı; “Şimdi güncelle” ile güncelleyin.',
    requested: 'Güncelleme istendi',
    updating: 'Güncelleniyor…',
    updatingTitle: 'Ajan {panel} sürümünü indirip yeniden başlıyor.',
    failed: 'Güncelleme başarısız',
    failedTitle: '{note}. “Şimdi güncelle” ile yeniden deneyebilirsiniz.',
    unsupported: 'Eski ajan',
    unsupportedTitle:
      'Ajan {v}, panel {panel}. Bu ajan otomatik güncellemeden önceki bir sürüm: kurulum komutunu bir kez yeniden çalıştırın, sonraki sürümler kendiliğinden gelir.',
    unsigned: 'İmzasız derleme',
    unsignedTitle: 'Ajan {v}, panel {panel}. Panelin bu derlemesi imzalı değil (yerel ya da test derlemesi); ajanlara güncelleme sunulamaz.',
    unversioned: 'Sürümsüz derleme',
    unversionedTitle:
      'Ajan {v}, panel {panel}. Biri sürüm numarası yerine commit özeti taşıyor (test derlemesi); otomatik güncelleme yalnızca yayınlanmış sürümler arasında çalışır.',
    newer: 'Panelden yeni',
    newerTitle: 'Ajan {v}, panel {panel}. Ajan daha yeni; sürüm düşürülmez.',
    off: 'Güncelleme kapalı',
    offTitle: 'Ajanda AUTO_UPDATE=0 ayarlı; kendini güncellemez.',
    readonly: 'Elle güncellenir',
    readonlyTitle: 'Ajan programının bulunduğu klasör yazılabilir değil (ör. uygulama imajıyla çalışıyor); imajı güncelleyin.',
    now: 'Şimdi güncelle',
    requestedToast: 'Güncelleme istendi; ajan birkaç saniye içinde yeni sürüme geçer',
    all: 'Tümünü güncelle',
    allTitle: '{n} ajan panelin sürümüne ({panel}) güncellenebilir',
    allToast: '{n} ajan için güncelleme istendi',
    auto: 'Otomatik güncelleme',
    autoInherit: 'Genel ayar ({state})',
    autoInheritPlain: 'Genel ayar',
    autoOn: 'Açık',
    autoOff: 'Kapalı',
    autoHelp: 'Panel yeni sürüme geçince ajan imzalı programı indirip kendini günceller. Genel ayar: Ayarlar → Genel.',
  },
  lockedTo: '{ip} IP’sine kilitli',
  lockPending: 'IP kilidi açık; ilk bağlantıda sabitlenir',
  actionsFor: '{name} için işlemler',
  /** {cmd}: güncelleme komutu (<code>). */
  foot: 'Son 90 saniyede sonuç gönderen kontrol noktası çevrimiçi sayılır. Ajanlar panelle birlikte kendiliğinden güncellenir (Ayarlar → Genel). “Eski ajan” görünenleri bir kez elle güncelleyin: sunucuda {cmd}, sonra kurulum komutunu tekrar çalıştırın (komut, satır menüsündeki “Token’ı yenile” ile yeniden alınır).',
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
    notifyOffline: 'Çevrimdışı kalınca bildir',
    notifyOfflineHelp:
      'Kontrol noktası 90 saniye boyunca istek göndermezse seçili kanallara “🔴 kontrol noktasına ulaşılamıyor” bildirimi gider ve bir olay açılır; tekrar bağlanınca “🟢” bildirimiyle olay kapanır. Kısa yeniden başlatmalar bildirim üretmez.',
    channels: 'Bildirim kanalları',
    /** {link}: kanal ekleme bağlantısı. */
    noChannels: 'Henüz bildirim kanalı yok. {link}',
    addChannel: 'Kanal ekleyin',
    channelOff: 'devre dışı',
    noChannelSelected: 'Kanal seçilmedi: bildirim gitmez, yalnızca olay kaydedilir.',
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
