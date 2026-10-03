// Bildirim kanalı tipleri ve alanları (lib/notifyTypes.ts) ile şemadan çizilen
// alanların ortak metinleri (NotificationForm.svelte, ConfigFields.svelte).
// Ürün adları (Telegram, Slack…), teknik belirteçler ve örnek adresler çevrilmez.
export default {
  /** Örnek adreslerdeki alan adı (placeholder, webhook örneği). */
  exampleDomain: 'ornek.com',
  groups: {
    messaging: 'Mesajlaşma',
    emailSms: 'E-posta ve SMS',
    mobile: 'Mobil bildirim',
    incident: 'Olay yönetimi',
    general: 'Genel',
  },
  /** Şemadan çizilen alanların ortak metinleri. */
  form: {
    optional: '(isteğe bağlı)',
    keptHelp: 'Kayıtlı değer korunur; değiştirmek için yenisini yazın.',
    keptKey: 'Kayıtlı anahtar korunuyor',
    keptValue: 'Kayıtlı değer korunuyor (gizli)',
    replace: 'Değiştir',
    keepKey: 'Vazgeç, kayıtlı anahtarı koru',
    keepValue: 'Vazgeç, kayıtlı değeri koru',
  },
  /** Alan doğrulama hataları ({field}: alan etiketi). */
  errors: {
    required: '{field} gerekli.',
    url: '{field} geçerli bir http(s) adresi olmalı.',
    invalid: '{field} geçersiz.',
    integer: '{field} bir tam sayı olmalı.',
    number: '{field} bir sayı olmalı.',
    range: '{field} {min}-{max} arasında olmalı.',
    pem: '{field} PEM biçiminde olmalı (-----BEGIN … ile başlar).',
  },
  whatsapp: {
    help: 'WP API üzerinden WhatsApp mesajı gönderir.',
    fields: {
      url: { label: 'WP API adresi' },
      api_key: { label: 'API anahtarı' },
      to: {
        label: 'Alıcı',
        placeholder: '905xxxxxxxxx veya 1203...@g.us',
        help: 'Ülke koduyla telefon numarası (+ olmadan) ya da @g.us ile biten grup kimliği.',
      },
      from_number_id: {
        label: 'Gönderen numara kimliği',
        help: 'Birden fazla bağlı numaranız varsa hangisinden gönderileceği.',
      },
    },
  },
  telegram: {
    help: '@BotFather ile bot oluşturun, botu gruba/kanala ekleyin ve sohbet kimliğini girin.',
    fields: {
      bot_token: { label: 'Bot token’ı' },
      chat_id: { label: 'Sohbet kimliği (chat ID)' },
      thread_id: {
        label: 'Konu kimliği (thread ID)',
        help: 'Konulara ayrılmış gruplarda mesajın gideceği konu.',
      },
    },
  },
  email: {
    label: 'E-posta (SMTP)',
    fields: {
      host: { label: 'SMTP sunucusu' },
      security: { label: 'Güvenlik', none: 'Yok (25)' },
      from: { label: 'Gönderen' },
      to: { label: 'Alıcılar', help: 'Birden fazla adresi virgülle ayırın.', placeholder: 'ben@ornek.com, ekip@ornek.com' },
    },
  },
  discord: {
    help: 'Kanal ayarları → Entegrasyonlar → Webhook oluşturun ve adresini yapıştırın.',
    fields: {
      webhook_url: { label: 'Webhook adresi' },
    },
  },
  slack: {
    help: 'Slack uygulamanızda “Incoming Webhooks” açıp oluşan adresi yapıştırın.',
    fields: {
      webhook_url: { label: 'Webhook adresi' },
    },
  },
  webhook: {
    /** Gönderilen JSON örneğindeki monitör adı. */
    exampleName: 'Web sitesi',
    fields: {
      url: { label: 'Adres' },
      method: { label: 'Metot' },
      headers: { label: 'Başlıklar', help: 'Her satıra bir başlık: “Ad: değer”. Gizli bilgi olarak saklanır.' },
      secret: { label: 'İmza anahtarı (HMAC)', help: 'Doluysa her istek X-Bekci-Signature başlığıyla imzalanır: t=<unix>,v1=HMAC-SHA256(anahtar, t + "." + gövde). Alıcı isteğin Bekci’den geldiğini doğrulayabilir.' },
    },
  },
  ntfy: {
    fields: {
      server: { label: 'Sunucu' },
      topic: { label: 'Konu (topic)', placeholder: 'benim-uptime-konum' },
      token: { label: 'Erişim token’ı', help: 'Korumalı konular için.' },
      priority: { label: 'Öncelik (1-5)', help: 'Sorun bildirimlerinde kullanılır.' },
    },
  },
  gotify: {
    fields: {
      server: { label: 'Sunucu' },
      app_token: { label: 'Uygulama token’ı' },
      priority: { label: 'Öncelik (1-10)' },
    },
  },
  pushover: {
    fields: {
      user_key: { label: 'Kullanıcı anahtarı' },
      app_token: { label: 'Uygulama token’ı' },
      device: { label: 'Cihaz', help: 'Boşsa tüm cihazlara gider.' },
      priority: { label: 'Öncelik', lowest: 'En düşük (-2)', low: 'Düşük (-1)', high: 'Yüksek (1)' },
    },
  },
  teams: {
    help: 'Teams kanalına Adaptive Card ile bildirim gönderir (Workflows webhook’u).',
    fields: {
      webhook_url: {
        label: 'Webhook adresi',
        help: 'Teams kanalında “Workflows” › “Post to a channel when a webhook request is received” akışı oluşturup verilen adresi yapıştırın (eski “Incoming Webhook” bağlayıcısı kapatıldı).',
      },
    },
  },
  googlechat: {
    help: 'Google Chat alanına (space) web kancası ile bildirim gönderir.',
    fields: {
      webhook_url: { label: 'Webhook adresi', help: 'Alan ayarlarından “Web kancaları” ile oluşturun.' },
    },
  },
  mattermost: {
    help: 'Mattermost gelen web kancasına bildirim gönderir.',
    fields: {
      webhook_url: { label: 'Webhook adresi', help: 'Sistem Konsolu › Entegrasyonlar › Gelen Web Kancaları' },
      channel: { label: 'Kanal', placeholder: '#uyarilar', help: 'Boşsa web kancasının kanalı.' },
      username: { label: 'Görünen ad' },
      icon_url: { label: 'Simge adresi', placeholder: 'https://ornek.com/simge.png' },
    },
  },
  rocketchat: {
    help: 'Rocket.Chat gelen web kancasına bildirim gönderir.',
    fields: {
      webhook_url: { label: 'Webhook adresi', help: 'Yönetim › Entegrasyonlar › Gelen' },
      channel: { label: 'Kanal', placeholder: '#uyarilar' },
      alias: { label: 'Görünen ad' },
      avatar: { label: 'Avatar adresi' },
    },
  },
  matrix: {
    help: 'Matrix odasına mesaj gönderir.',
    fields: {
      homeserver_url: { label: 'Sunucu adresi' },
      access_token: {
        label: 'Erişim jetonu',
        help: 'Element › Ayarlar › Yardım ve Hakkında › Gelişmiş › Erişim Jetonu (tercihen ayrı bir bot hesabı).',
      },
      room_id: { label: 'Oda kimliği', help: 'Oda ayarları › Gelişmiş (takma ad değil).' },
    },
  },
  signal: {
    help: 'signal-cli-rest-api sunucusu üzerinden Signal mesajı gönderir.',
    fields: {
      url: { label: 'Sunucu adresi' },
      number: { label: 'Gönderen numara' },
      recipients: { label: 'Alıcılar', help: 'Birden fazla alıcıyı virgülle ayırın.' },
    },
  },
  pagerduty: {
    help: 'PagerDuty’de Events API v2 ile olay açar ve düzelince çözer.',
    fields: {
      routing_key: { help: 'Servisin “Events API v2” entegrasyon anahtarı.' },
      severity: {
        label: 'Önem derecesi',
        help: 'SSL sertifikası uyarıları her zaman “warning” gönderilir.',
        critical: 'Kritik (critical)',
        error: 'Hata (error)',
        warning: 'Uyarı (warning)',
        info: 'Bilgi (info)',
      },
    },
  },
  opsgenie: {
    help: 'Opsgenie’de alarm açar ve düzelince kapatır.',
    fields: {
      api_key: { label: 'API anahtarı', help: 'Takımlar › Entegrasyonlar › API' },
      region: { label: 'Bölge', us: 'ABD (us)', eu: 'Avrupa (eu)' },
      priority: {
        label: 'Öncelik',
        p1: 'P1 — Kritik',
        p2: 'P2 — Yüksek',
        p3: 'P3 — Orta',
        p4: 'P4 — Düşük',
        p5: 'P5 — Bilgi',
      },
    },
  },
  homeassistant: {
    help: 'Home Assistant notify servisi üzerinden bildirim gönderir (mobil uygulama vb.).',
    fields: {
      url: { label: 'Home Assistant adresi' },
      token: { label: 'Uzun ömürlü erişim jetonu', help: 'Profil › Güvenlik › Uzun Ömürlü Erişim Jetonları' },
      service: {
        label: 'Bildirim servisi',
        help: '“notify.” öneki olmadan.',
        pattern: 'Bildirim servisi yalnızca küçük harf, rakam ve alt çizgi içerebilir (notify. öneki olmadan).',
      },
    },
  },
  netgsm: {
    help: 'Netgsm üzerinden Türkiye numaralarına SMS gönderir.',
    fields: {
      usercode: { label: 'Kullanıcı kodu' },
      password: { label: 'API şifresi' },
      msgheader: { label: 'Gönderici başlığı', placeholder: 'FIRMA', help: 'Netgsm’de onaylı başlık.' },
      gsm: { label: 'Telefon numaraları', help: 'Virgülle ayırın. 0, 90 ve +90 önekleri otomatik düzeltilir.' },
      turkish_chars: {
        label: 'Türkçe karakterler',
        help: 'Açıksa ç, ğ, ı, ö, ş, ü korunur (SMS başına karakter hakkı azalabilir).',
      },
    },
  },
  twilio: {
    help: 'Twilio ile SMS gönderir.',
    fields: {
      from: { label: 'Gönderen numara', help: 'Ülke koduyla, + ile başlayarak.' },
      to: { label: 'Alıcı numaralar', help: 'Virgülle ayırın; her numara + ve ülke koduyla.' },
    },
  },
  pushbullet: {
    help: 'Pushbullet bildirimi gönderir.',
    fields: {
      access_token: { label: 'Erişim jetonu', help: 'Ayarlar › Erişim Jetonları' },
      channel_tag: { label: 'Kanal etiketi', help: 'Cihaz kimliği ile birlikte kullanılamaz.' },
      device_iden: { label: 'Cihaz kimliği', help: 'Boşsa tüm cihazlarınıza gider.' },
    },
  },
  bark: {
    help: 'Bark uygulamasına iOS bildirimi gönderir.',
    fields: {
      server: { label: 'Sunucu adresi' },
      device_key: { label: 'Cihaz anahtarı' },
      sound: { label: 'Ses' },
      group: { label: 'Grup' },
    },
  },
  line: {
    help: 'LINE Messaging API ile mesaj gönderir (LINE Notify kapatıldı).',
    fields: {
      channel_access_token: { label: 'Kanal erişim jetonu' },
      to: {
        label: 'Alıcı kimliği',
        help: 'Kullanıcı (U…), grup (C…) veya oda (R…) kimliği.',
        pattern: 'Alıcı ID’si U, C veya R ile başlayan 33 karakterlik bir kimlik olmalı.',
      },
    },
  },
  webpush: {
    label: 'Tarayıcı bildirimi (Web Push)',
    help: 'Panele giriş yapan kullanıcıların tarayıcılarına ve ana ekrana eklenmiş uygulamaya doğrudan bildirim gönderir. Kullanıcılar Ayarlar → Hesabım → Tarayıcı bildirimleri’nden cihazlarını kaydeder; müşteri hesapları yalnızca görebildikleri monitörlerin bildirimini alır.',
    fields: {
      users: { label: 'Kullanıcılar', help: 'Virgülle ayrılmış kullanıcı adları. Boş bırakılırsa cihaz kaydetmiş herkese gider.' },
    },
  },
  apprise: {
    help: 'Kendi Apprise API sunucunuz üzerinden 100’den fazla servise gönderir.',
    fields: {
      server: { label: 'Apprise sunucu adresi' },
      urls: { label: 'Apprise URL’leri', help: 'Birden fazla adresi virgülle ayırın. Gizli bilgi olarak saklanır.' },
    },
  },
};
