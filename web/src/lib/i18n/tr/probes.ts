// Ayarlar › Kontrol noktaları (Probes.svelte).
export default {
  title: "Kontrol noktaları",
  /** {probe}: <code>probe</code>; {locations}: kalın "Konumlar" (locations). */
  intro:
    "Monitörlerinizi farklı şehir veya ağlardan da kontrol edin. Kontrol noktası, bu uygulamanın başka bir sunucuda {probe} modunda çalışan bir kopyasıdır; kendisine atanan monitörleri kontrol edip sonuçları buraya bildirir. Monitör formundaki {locations} bölümünden atanır.",
  locations: "Konumlar",
  newProbe: "Yeni kontrol noktası",
  empty:
    "Henüz kontrol noktası yok. Tüm kontroller şu an yalnızca bu sunucudan yapılıyor.",
  state: {
    disabled: "Devre dışı",
    online: "Çevrimiçi",
    offline: "Çevrimdışı",
  },
  col: {
    tokenPrefix: "Token öneki",
    status: "Durum",
    lastSeen: "Son görülme",
    address: "Adres",
    monitors: "Monitör",
  },
  neverConnected: "Hiç bağlanmadı",
  update: {
    current: "Güncel",
    currentTitle: "Ajan panelle aynı sürümde ({panel}).",
    outdated: "Güncelleme var",
    outdatedAuto:
      "Ajan {v}, panel {panel}. Otomatik güncelleme açık: ajan birkaç saniye içinde yeni sürüme geçer.",
    outdatedManual:
      "Ajan {v}, panel {panel}. Bu ajanda otomatik güncelleme kapalı; “Şimdi güncelle” ile güncelleyin.",
    requested: "Güncelleme istendi",
    updating: "Güncelleniyor…",
    updatingTitle: "Ajan {panel} sürümünü indirip yeniden başlıyor.",
    failed: "Güncelleme başarısız",
    failedTitle: "{note}. “Şimdi güncelle” ile yeniden deneyebilirsiniz.",
    unsupported: "Eski ajan",
    unsupportedTitle:
      "Ajan {v}, panel {panel}. Bu ajan otomatik güncellemeden önceki bir sürüm: kurulum komutunu bir kez yeniden çalıştırın, sonraki sürümler kendiliğinden gelir.",
    unsigned: "İmzasız derleme",
    unsignedTitle:
      "Ajan {v}, panel {panel}. Panelin bu derlemesi imzalı değil (yerel ya da test derlemesi); ajanlara güncelleme sunulamaz.",
    unversioned: "Sürümsüz derleme",
    unversionedTitle:
      "Ajan {v}, panel {panel}. Biri sürüm numarası yerine commit özeti taşıyor (test derlemesi); otomatik güncelleme yalnızca yayınlanmış sürümler arasında çalışır.",
    newer: "Panelden yeni",
    newerTitle: "Ajan {v}, panel {panel}. Ajan daha yeni; sürüm düşürülmez.",
    off: "Güncelleme kapalı",
    offTitle: "Ajanda AUTO_UPDATE=0 ayarlı; kendini güncellemez.",
    readonly: "Elle güncellenir",
    readonlyTitle:
      "Ajan programının bulunduğu klasör yazılabilir değil (ör. uygulama imajıyla çalışıyor); imajı güncelleyin.",
    now: "Şimdi güncelle",
    requestedToast:
      "Güncelleme istendi; ajan birkaç saniye içinde yeni sürüme geçer",
    all: "Tümünü güncelle",
    allTitle: "{n} ajan panelin sürümüne ({panel}) güncellenebilir",
    allToast: "{n} ajan için güncelleme istendi",
    auto: "Otomatik güncelleme",
    autoInherit: "Genel ayar ({state})",
    autoInheritPlain: "Genel ayar",
    autoOn: "Açık",
    autoOff: "Kapalı",
    autoHelp:
      "Panel yeni sürüme geçince ajan imzalı programı indirip kendini günceller. Genel ayar: Ayarlar → Genel.",
  },
  lockedTo: "{ip} IP’sine kilitli",
  lockPending: "IP kilidi açık; ilk bağlantıda sabitlenir",
  actionsFor: "{name} için işlemler",
  /** {cmd}: güncelleme komutu (<code>). */
  foot: "Art arda kopukluk eşiği kadar kalp atışı gelmeyen kontrol noktası çevrimdışı sayılır (varsayılan 3 x 5 sn = 15 sn; eski ajanlarda 90 sn). Ajanlar panelle birlikte kendiliğinden güncellenir (Ayarlar → Genel). “Eski ajan” görünenleri bir kez elle güncelleyin: sunucuda {cmd}, sonra kurulum komutunu tekrar çalıştırın (komut, satır menüsündeki “Token’ı yenile” ile yeniden alınır).",
  menu: {
    enable: "Etkinleştir",
    disable: "Devre dışı bırak",
    regenerate: "Token’ı yenile",
  },
  confirm: {
    regenerateTitle: "Token’ı yenile",
    regenerateMessage:
      "“{name}” — Eski token hemen geçersiz olur; kontrol noktasını yeni token ile yeniden başlatın.",
    regenerateConfirm: "Token’ı yenile",
    disableTitle: "Devre dışı bırak",
    disableMessage:
      "“{name}” sonuç gönderemeyecek ve konum hesaplarına katılmayacak. Daha sonra yeniden etkinleştirebilirsiniz.",
    disableConfirm: "Devre dışı bırak",
    deleteTitle: "Kontrol noktasını sil",
    deleteMessage:
      "“{name}” — Bu kontrol noktası tüm monitörlerden çıkarılacak.",
  },
  toast: {
    saved: "Kontrol noktası kaydedildi",
    ipReset: "IP kilidi sıfırlandı",
    disabled: "Kontrol noktası devre dışı bırakıldı",
    enabled: "Kontrol noktası etkinleştirildi",
    deleted: "Kontrol noktası silindi",
  },
  form: {
    errName: "Kontrol noktasına bir ad verin (ör. Frankfurt).",
    errNameRequired: "Ad gerekli.",
    region: "Bölge",
    regionPlaceholder: "ör. Avrupa",
    provider: "Sağlayıcı",
    providerPlaceholder: "ör. Hetzner",
    code: "Kısa kod",
    codePlaceholder: "ör. FRA1",
    labelsHelp: "Hepsi isteğe bağlı; canlı topolojide gruplama ve kısa gösterim için.",
    namePlaceholder: "ör. Frankfurt",
    nameHelp:
      "Konumu anlatan kısa bir ad; monitör detayında ve bildirimlerde görünür.",
    editTitle: "Kontrol noktasını düzenle",
    ipLock: "IP’ye kilitle",
    ipLockHelp:
      "Kontrol noktası yalnızca ilk bağlandığı IP’den sonuç gönderebilir; sunucu taşınırsa kilidi sıfırlayın.",
    /** {ip}: kalın kilitli IP. */
    lockedIp: "Kilitli IP: {ip}",
    resetLock: "Kilidi sıfırla",
    notifyOffline: "Çevrimdışı kalınca bildir",
    notifyOfflineHelp:
      "Kontrol noktasından art arda kopukluk eşiği kadar kalp atışı gelmezse (varsayılan 15 sn) seçili kanallara “kontrol noktasına ulaşılamıyor” bildirimi gider ve bir olay açılır; tekrar bağlanınca düzelme bildirimiyle olay kapanır. Bu süreden kısa yeniden başlatmalar bildirim üretmez.",
    channels: "Bildirim kanalları",
    /** {link}: kanal ekleme bağlantısı. */
    noChannels: "Henüz bildirim kanalı yok. {link}",
    addChannel: "Kanal ekleyin",
    channelOff: "devre dışı",
    noChannelSelected:
      "Kanal seçilmedi: bildirim gitmez, yalnızca olay kaydedilir.",
  },
  mesh: {
    title: "Bağlantı denetimi (mesh)",
    intro:
      "Kontrol noktaları birbirine ve panele düzenli ping atar; bağlantı kopukluğu ile gerçek kesinti ayırt edilir. Değerler her kontrol noktasında ayrıca ezilebilir.",
    enabled: "Mesh açık",
    enabledHelp:
      "Kapalıyken kontrol noktaları birbirine ping atmaz; yalnızca panelle haberleşme izlenir.",
    pingSec: "Mesh ping aralığı (sn)",
    pingSecHelp: "1-60, varsayılan 5",
    lossThreshold: "Kopukluk eşiği (ardışık kayıp)",
    lossThresholdHelp: "1-10, varsayılan 3",
    panelSec: "Panel haberleşme aralığı (sn)",
    panelSecHelp: "1-60, varsayılan 5",
    /** {n}: saniye; {loss}: eşik; {panel}: panel aralığı. */
    offlineAfter:
      "Çevrimdışı sayılma süresi = eşik x panel aralığı = {loss} x {panel} = {n} sn",
    saved: "Mesh ayarları kaydedildi",
    errPing: "Mesh ping aralığı 1-60 sn olmalı.",
    errLoss: "Kopukluk eşiği 1-10 olmalı.",
    errPanel: "Panel haberleşme aralığı 1-60 sn olmalı.",
    errWindow:
      "Kopukluk eşiği x aralık en az 3 sn olmalı (ping ve panel aralığı için).",
    errAddr: "Mesh adresi host:port biçiminde olmalı (ya da boş / -).",
    probeTitle: "Mesh",
    useGlobal: "Genel değeri kullan ({v})",
    addr: "Mesh adresi",
    addrPlaceholder: "boş: görülen IP:7480, -: dinlemiyor",
    addrHelp:
      "Diğer kontrol noktalarının bu noktaya bağlanacağı host:port. NAT arkasındaysa - yazın.",
    fallback: "Yedek bildirici",
    fallbackHelp:
      "Main’e ulaşılamazsa bu kontrol noktası bildirim gönderebilir (özellik sonraki sürümde etkin).",
    effective: "Etkin: ping {ping} sn, eşik {loss}, panel {panel} sn",
  },
  setup: {
    addedTitle: "“{name}” eklendi",
    newTokenTitle: "“{name}” için yeni token",
    shownOnce: "Bu token yalnızca bir kez gösterilir; kopyalayıp saklayın.",
    token: "Token",
    command: "Kurulum komutu",
    /** {root}: kalın "root"; {sudo}: <code>sudo sh <<'UPTIME_KURULUM'</code>. */
    commandHelp:
      "Kontrol noktası olacak sunucuda komutun tamamını {root} olarak yapıştırın (ya da ilk satırı {sudo} yapın).",
    /** {path}: <code>/etc/uptime-probe.env</code>; {cmd}: güncelleme komutu. */
    commandAfter:
      "Token sunucuda {path} dosyasında (600 izinle) tutulur. Güncellemek için: {cmd}, sonra komutu tekrar çalıştırın.",
    /** {url}: <code> sunucu adresi; {online}: kalın "Çevrimiçi". */
    serverUrl:
      "Sunucu adresi: {url}. Kontrol noktası bu adrese dışarıdan erişebilmeli; birkaç saniye içinde listede {online} görünür.",
    copiedClose: "Kopyaladım, kapat",
  },
  uplink: {
    primary: "Ana hat",
    via: "{name} üzerinden",
    primaryTip: "Ajan ana hat üzerinden bağlı. Ana hat Main'e ulaşamazsa {name} hattına geçer.",
    viaTip: "Ana hat Main'e ulaşamıyor; ajan {name} ({ip}) üzerinden bağlı. Geçiş: {at}. Bu hattan gelen sonuçlar karara katılmaz.",
    ifaceMissing: "OOB arayüzü bulunamadı",
    ifaceMissingTip: "Ajan OOB IP'sinin bağlı olduğu ağ arayüzünü bulamadı. Ajanda PROBE_OOB_IFACE ile arayüz adını verin.",
  },
  oob: {
    title: "OOB (ikinci hat)",
    help: "Ana hat Main'e ulaşamazsa ajan bu IP'nin bağlı olduğu arayüzden bağlanır. IP kilidi bu adresi de kabul eder. Boş bırakırsanız OOB kapalıdır.",
    ip: "OOB IP adresi",
    ipPlaceholder: "ör. 203.0.113.10",
    name: "Hat adı",
    namePlaceholder: "OOB",
    nameHelp: "Bildirimlerde ve rozette görünür; en çok 40 karakter.",
    errIp: "Geçerli bir IPv4 ya da IPv6 adresi girin.",
    errName: "Hat adı en çok 40 karakter olabilir.",
  },
  topo: {
    label: "Kontrol noktası bağlantı şeması",
    healthy: "Çift yönlü erişim başarılı",
    /** Bağlantı ipucunun ilk satırı: {a}, {b} düğüm adları; {state} çift durumu. */
    pair: "{a} - {b}: {state}",
    dir: "{src} > {dst}: {state}",
    loss: "ardışık kayıp: {n}",
    lastOk: "son başarı {when}",
    never: "hiç başarılı ölçüm yok",
    state: { up: "Sağlıklı", down: "Koptu", oneway: "Tek yönlü", unknown: "Ölçülemiyor" },
    count: { up: "{n} sağlıklı", down: "{n} koptu", oneway: "{n} tek yönlü", unknown: "{n} ölçülemiyor" },
  },
  matrix: {
    title: "Bağlantı durumu",
    intro: "Satırdaki nokta sütundakine ulaşabiliyor mu. Panel aralığında bir canlı tazelenir.",
    inactive: "Bağlantı durumu için mesh açık olmalı ve en az iki kontrol noktası güncel ajanla kalp atışı göndermeli.",
    healthy: "Tüm bilinen bağlantılar sağlıklı.",
    from: "Kaynak",
    up: "Ulaşıyor",
    down: "Ulaşamıyor",
    unknown: "Ölçülemiyor",
    cell: "{src} > {dst}: {state}",
  },
};
