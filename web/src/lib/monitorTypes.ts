// Monitör tipleri kayıt defteri.
//
// Yeni bir tip eklemek için (ör. veritabanı, docker, grpc, mqtt, smtp …):
//  1. api.ts'teki MonitorType birliğine sunucudaki tip adını ekleyin.
//  2. Aşağıdaki MONITOR_TYPES listesine bir kayıt ekleyin. `fields` verilirse
//     form bu alanları genel alan çiziciyle (ConfigFields) gösterir ve config
//     nesnesini alan anahtarlarından kurar; özel bir arayüz gerekiyorsa
//     MonitorForm.svelte'e o tip için bir bölüm eklenir.
//  3. Tip sunucuda gizli alan döndürüyorsa (maskeli "••••••") alanı `secret`
//     yapın; değer değiştirilmezse maske aynen geri gönderilir.
//
// Alan kuralları: zorunlu ve sık kullanılan alanlar ana bölümde, geri kalanı
// `advanced: true` ile "Gelişmiş ayarlar"da. `showIf` ile koşullu alanlar
// gizliyken varsayılan değerleriyle gönderilir (sunucu çoğunu zaten temizler).

import type { MonitorType } from './api';
import type { Field, FieldKind } from './notifyTypes';
import type { IconName } from '../components/Icon.svelte';

export type TypeCategory = 'web' | 'network' | 'database' | 'system';

export const CATEGORY_LABELS: Record<TypeCategory, string> = {
  web: 'Web',
  network: 'Ağ ve protokoller',
  database: 'Veritabanı',
  system: 'Sistem ve sinyaller',
};

export const CATEGORY_ORDER: TypeCategory[] = ['web', 'network', 'database', 'system'];

/** Monitör ayar alanı: bildirim alanlarına ek olarak PEM, koşul ve bölüm bilgisi. */
export interface CfgField extends Omit<Field, 'kind'> {
  kind: FieldKind | 'pem';
  /** PEM alanı sunucuda gizli tutuluyor (maskeli döner). */
  secret?: boolean;
  /** "Gelişmiş ayarlar" bölümünde gösterilir. */
  advanced?: boolean;
  /** Yalnızca koşul sağlanınca gösterilir; gizliyken varsayılan değeri gönderilir. */
  showIf?: (v: Record<string, string>) => boolean;
  /** Bu alandan önce gösterilecek alt başlık (ve isteğe bağlı açıklaması). */
  section?: string;
  sectionHelp?: string;
  /** Tek satırlık alan eş aralıklı yazı tipiyle gösterilir (OID, seçici vb.). */
  mono?: boolean;
  /** Ad boşken bu alandan ad önerilir. */
  suggestName?: boolean;
}

export interface MonitorTypeDef {
  key: MonitorType;
  /** Tip seçicideki ad. */
  label: string;
  /** Listelerdeki kısa rozet. */
  badge: string;
  /** Tip seçicideki tek satırlık açıklama. */
  desc: string;
  /** Formda alanların üstünde gösterilen açıklama. */
  about?: string;
  /** Formda gösterilen uyarı/ipucu kutusu. */
  note?: string;
  icon: IconName;
  category: TypeCategory;
  /** Arama için ek anahtar kelimeler. */
  keywords?: string;
  /** Zaman aşımı ayarı anlamlı mı (push ve grup kendi başına istek atmaz). */
  timeout?: boolean;
  /** "Ters mod" anlamlı mı. */
  upsideDown?: boolean;
  /** Kontrol noktalarında (uzak konumlarda) çalıştırılabilir mi. */
  remote?: boolean;
  /** Özel form bölümü olmayan tipler için genel alanlar. */
  fields?: CfgField[];
}

// Ortak alan parçaları --------------------------------------------------------------------

const PORT_RE = /^\d{1,5}$/;

const host = (ph = 'db.ornek.com'): CfgField => ({
  key: 'host',
  label: 'Sunucu adresi',
  kind: 'text',
  placeholder: ph,
  required: true,
  suggestName: true,
});

const port = (def: number, extra: Partial<CfgField> = {}): CfgField => ({
  key: 'port',
  label: 'Port',
  kind: 'number',
  def,
  min: 1,
  max: 65535,
  pattern: PORT_RE,
  patternMsg: 'Port 1-65535 arasında olmalı.',
  ...extra,
});

const ignoreTls = (extra: Partial<CfgField> = {}): CfgField => ({
  key: 'ignore_tls',
  label: 'Sertifika doğrulamasını atla',
  kind: 'bool',
  help: 'Kendinden imzalı veya süresi dolmuş sertifikada da bağlan.',
  advanced: true,
  ...extra,
});

/** MySQL, PostgreSQL ve MSSQL'in ortak alanları. */
function sqlFields(def: number, tls: CfgField): CfgField[] {
  return [
    host(),
    port(def),
    { key: 'username', label: 'Kullanıcı adı', kind: 'text', placeholder: 'izleme' },
    { key: 'password', label: 'Şifre', kind: 'secret' },
    { key: 'database', label: 'Veritabanı', kind: 'text', optional: true, wide: true },
    {
      key: 'query',
      label: 'Sorgu',
      kind: 'text',
      def: 'SELECT 1',
      mono: true,
      wide: true,
      advanced: true,
    },
    {
      key: 'expected',
      label: 'Beklenen sonuç',
      kind: 'text',
      optional: true,
      advanced: true,
      help: 'Boşsa sorgunun başarılı çalışması yeterli; doluysa ilk satırın ilk sütunu bununla eşleşmeli.',
    },
    { ...tls, advanced: true },
  ];
}

const JSON_OPS_MQTT = [
  { v: '==', l: 'eşittir (==)' },
  { v: '!=', l: 'eşit değildir (!=)' },
  { v: 'contains', l: 'içerir' },
  { v: '>', l: 'büyüktür (>)' },
  { v: '>=', l: 'büyük veya eşit (>=)' },
  { v: '<', l: 'küçüktür (<)' },
  { v: '<=', l: 'küçük veya eşit (<=)' },
];

const hasTopic = (v: Record<string, string>) => !!v.topic?.trim();
const isV3 = (v: Record<string, string>) => v.version === 'v3';

// Kayıt defteri ---------------------------------------------------------------------------

export const MONITOR_TYPES: MonitorTypeDef[] = [
  // Web
  {
    key: 'http',
    label: 'HTTP(S)',
    badge: 'HTTP',
    desc: 'Web sitesi veya API adresini kontrol eder',
    icon: 'globe',
    category: 'web',
    keywords: 'web site api url https keyword json',
  },
  {
    key: 'browser',
    label: 'Tarayıcı (gerçek Chrome)',
    badge: 'TARAYICI',
    desc: 'Sayfayı gerçek Chrome’da açıp içeriği kontrol eder',
    about:
      'Sayfayı gerçek bir Chrome’da açar; JavaScript sonrası içeriği, HTTP durumunu ve konsol hatalarını kontrol eder.',
    note: 'Ağır bir kontroldür; sadece kritik sayfalar için ve 5-10 dakikalık aralıklarla kullanın. Ayrı bir tarayıcı container’ı gerektirir.',
    icon: 'browser',
    category: 'web',
    keywords: 'chrome chromium playwright puppeteer spa javascript',
    fields: [
      { key: 'url', label: 'Adres', kind: 'url', placeholder: 'https://ornek.com', required: true, wide: true, suggestName: true },
      {
        key: 'keyword',
        label: 'Beklenen kelime',
        kind: 'text',
        optional: true,
        wide: true,
        help: 'Sayfanın görünür metninde bu kelime yoksa çalışmıyor sayılır.',
      },
      {
        key: 'wait_selector',
        label: 'Beklenecek CSS seçici',
        kind: 'text',
        optional: true,
        mono: true,
        placeholder: '#app .hazir',
        advanced: true,
        help: 'Doluysa bu öğe sayfada görünene kadar beklenir.',
      },
      {
        key: 'endpoint',
        label: 'Tarayıcı uç noktası (isteğe bağlı)',
        kind: 'text',
        mono: true,
        placeholder: 'ws://chrome:3000',
        advanced: true,
        help: 'Boşsa sunucudaki BROWSER_WS_ENDPOINT kullanılır. Kurulum: README › Tarayıcı monitörü',
      },
      ignoreTls(),
      {
        key: 'fail_on_console_errors',
        label: 'Konsol hatalarında çalışmıyor say',
        kind: 'bool',
        help: 'Sayfada JavaScript hatası oluşursa monitör çalışmıyor sayılır.',
        advanced: true,
      },
    ],
  },

  // Ağ ve protokoller
  { key: 'tcp', label: 'TCP Port', badge: 'TCP', desc: 'Sunucudaki bir portun açık olduğunu kontrol eder', icon: 'plug', category: 'network' },
  { key: 'ping', label: 'Ping', badge: 'PING', desc: 'Sunucunun ağdan yanıt verdiğini kontrol eder', icon: 'radio', category: 'network', keywords: 'icmp' },
  { key: 'dns', label: 'DNS', badge: 'DNS', desc: 'Alan adının DNS kaydını sorgular', icon: 'server', category: 'network' },
  {
    key: 'tlscert',
    label: 'TLS sertifikası',
    badge: 'TLS',
    desc: 'HTTP olmayan servislerin sertifika bitişini izler',
    about: 'HTTP olmayan bir TLS servisinin sertifikasını ve bitiş tarihini izler.',
    icon: 'certificate',
    category: 'network',
    keywords: 'ssl sertifika imap ldaps',
    fields: [
      host('mail.ornek.com'),
      port(443, { required: true, help: 'Ör. 443, 993 (IMAPS), 636 (LDAPS), 8443.' }),
      {
        key: 'server_name',
        label: 'SNI adı',
        kind: 'text',
        optional: true,
        advanced: true,
        help: 'Boşsa sunucu adresi kullanılır.',
      },
      ignoreTls({ help: 'Doğrulanamayan sertifikanın da bitiş tarihini izle.' }),
    ],
  },
  {
    key: 'smtp',
    label: 'SMTP (posta sunucusu)',
    badge: 'SMTP',
    desc: 'Posta sunucusunun banner ve EHLO akışını doğrular',
    about: 'Posta sunucusuna bağlanır, banner ve EHLO/STARTTLS akışını doğrular.',
    icon: 'mail',
    category: 'network',
    keywords: 'e-posta mail eposta starttls',
    fields: [
      host('mail.ornek.com'),
      port(25, { help: 'Genellikle 25 veya 587 (STARTTLS), 465 (TLS).' }),
      {
        key: 'security',
        label: 'Güvenlik',
        kind: 'select',
        def: 'none',
        wide: true,
        options: [
          { v: 'none', l: 'Yok (düz bağlantı)' },
          { v: 'starttls', l: 'STARTTLS' },
          { v: 'tls', l: 'TLS (doğrudan)' },
        ],
      },
      ignoreTls({ showIf: (v) => v.security !== 'none' }),
      {
        key: 'expected_banner',
        label: 'Beklenen banner metni',
        kind: 'text',
        optional: true,
        advanced: true,
        placeholder: 'ESMTP',
        help: 'Doluysa sunucunun karşılama mesajı bu metni içermeli.',
      },
    ],
  },
  {
    key: 'websocket',
    label: 'WebSocket',
    badge: 'WS',
    desc: 'WebSocket sunucusuna bağlanıp yanıtı kontrol eder',
    about: 'WebSocket sunucusuna bağlanır, isteğe bağlı mesaj gönderip yanıtı kontrol eder.',
    icon: 'arrows-lr',
    category: 'network',
    keywords: 'ws wss socket',
    fields: [
      {
        key: 'url',
        label: 'Adres',
        kind: 'text',
        placeholder: 'wss://ornek.com/ws',
        required: true,
        wide: true,
        mono: true,
        suggestName: true,
        pattern: /^wss?:\/\/\S+$/i,
        patternMsg: 'Adres ws:// veya wss:// ile başlamalı.',
      },
      {
        key: 'send',
        label: 'Gönderilecek mesaj',
        kind: 'text',
        optional: true,
        wide: true,
        mono: true,
        placeholder: '{"type":"ping"}',
        advanced: true,
        section: 'Mesaj',
      },
      {
        key: 'keyword',
        label: 'Beklenen kelime',
        kind: 'text',
        optional: true,
        wide: true,
        advanced: true,
        help: 'Doluysa sunucudan gelen ilk mesaj bu metni içermeli.',
      },
      {
        key: 'headers',
        label: 'Başlıklar',
        kind: 'textarea',
        secret: true,
        optional: true,
        placeholder: 'Authorization: Bearer abc123',
        advanced: true,
        help: 'Her satıra bir başlık: Ad: değer',
      },
      ignoreTls(),
    ],
  },
  {
    key: 'grpc',
    label: 'gRPC sağlık kontrolü',
    badge: 'GRPC',
    desc: 'grpc.health.v1 ile gRPC servisinin durumunu sorar',
    about: 'grpc.health.v1 servisi üzerinden gRPC sunucusunun durumunu kontrol eder.',
    icon: 'cpu',
    category: 'network',
    keywords: 'grpc health protobuf',
    fields: [
      {
        key: 'target',
        label: 'Sunucu adresi',
        kind: 'text',
        placeholder: 'api.ornek.com:50051',
        required: true,
        mono: true,
        suggestName: true,
        pattern: /^\S+:\d{1,5}$/,
        patternMsg: 'Sunucu adresi host:port biçiminde olmalı (ör. api.ornek.com:50051).',
      },
      { key: 'service', label: 'Servis adı', kind: 'text', optional: true, mono: true, help: 'Boşsa sunucunun genel durumu sorulur.' },
      { key: 'tls', label: 'TLS kullan', kind: 'bool' },
      ignoreTls({ showIf: (v) => v.tls === 'true' }),
      {
        key: 'metadata',
        label: 'Metadata başlıkları',
        kind: 'textarea',
        secret: true,
        optional: true,
        placeholder: 'authorization: Bearer abc123',
        advanced: true,
        help: 'Her satıra bir başlık: Ad: değer',
      },
    ],
  },
  {
    key: 'mqtt',
    label: 'MQTT',
    badge: 'MQTT',
    desc: 'Broker’a bağlanır, isteğe bağlı konudan mesaj bekler',
    about: 'MQTT broker’ına bağlanır; isteğe bağlı bir konuya abone olup mesaj bekler.',
    icon: 'rss',
    category: 'network',
    keywords: 'mosquitto iot broker',
    fields: [
      {
        key: 'broker_url',
        label: 'Broker adresi',
        kind: 'text',
        placeholder: 'tcp://broker.ornek.com:1883',
        required: true,
        wide: true,
        mono: true,
        suggestName: true,
        pattern: /^(tcp|ssl|tls|ws|wss|mqtt|mqtts):\/\/\S+$/i,
        patternMsg: 'Broker adresi tcp://, ssl://, tls://, ws:// veya wss:// ile başlamalı.',
        help: 'tcp://, ssl://, tls://, ws:// veya wss://',
      },
      { key: 'username', label: 'Kullanıcı adı', kind: 'text', optional: true },
      { key: 'password', label: 'Şifre', kind: 'secret', optional: true },
      {
        key: 'topic',
        label: 'Konu (topic)',
        kind: 'text',
        optional: true,
        wide: true,
        mono: true,
        placeholder: 'sensor/durum',
        advanced: true,
        section: 'Mesaj kontrolü',
        sectionHelp: 'Konu boşsa yalnızca bağlantı kurulabildiği kontrol edilir.',
      },
      {
        key: 'keyword',
        label: 'Beklenen kelime',
        kind: 'text',
        optional: true,
        wide: true,
        advanced: true,
        showIf: hasTopic,
        help: 'Doluysa konudan gelen mesaj bu metni içermeli.',
      },
      {
        key: 'json_path',
        label: 'JSON yolu',
        kind: 'text',
        optional: true,
        mono: true,
        placeholder: 'data.status',
        advanced: true,
        showIf: hasTopic,
      },
      {
        key: 'json_op',
        label: 'Karşılaştırma',
        kind: 'select',
        def: '==',
        options: JSON_OPS_MQTT,
        advanced: true,
        showIf: (v) => hasTopic(v) && !!v.json_path?.trim(),
      },
      {
        key: 'json_expected',
        label: 'Beklenen değer',
        kind: 'text',
        optional: true,
        advanced: true,
        showIf: (v) => hasTopic(v) && !!v.json_path?.trim(),
      },
      ignoreTls(),
    ],
  },
  {
    key: 'snmp',
    label: 'SNMP',
    badge: 'SNMP',
    desc: 'Ağ cihazından SNMP ile bir OID değeri okur',
    about: 'Ağ cihazından SNMP ile bir OID değeri okur.',
    icon: 'router',
    category: 'network',
    keywords: 'switch router oid mib',
    fields: [
      host('10.0.0.1'),
      port(161),
      {
        key: 'version',
        label: 'SNMP sürümü',
        kind: 'select',
        def: 'v2c',
        options: [
          { v: 'v1', l: 'v1' },
          { v: 'v2c', l: 'v2c' },
          { v: 'v3', l: 'v3 (kullanıcı tabanlı)' },
        ],
      },
      {
        key: 'community',
        label: 'Community',
        kind: 'secret',
        placeholder: 'public',
        help: 'Boşsa “public” kullanılır.',
        showIf: (v) => !isV3(v),
      },
      {
        key: 'oid',
        label: 'OID',
        kind: 'text',
        placeholder: '1.3.6.1.2.1.1.3.0',
        required: true,
        mono: true,
        pattern: /^\.?\d+(\.\d+)+$/,
        patternMsg: 'Geçerli bir OID girin (örnek: 1.3.6.1.2.1.1.3.0).',
        wide: true,
      },
      {
        key: 'username',
        label: 'Kullanıcı adı',
        kind: 'text',
        required: true,
        showIf: isV3,
        section: 'SNMPv3 kimlik bilgileri',
      },
      {
        key: 'auth_protocol',
        label: 'Kimlik doğrulama protokolü',
        kind: 'select',
        def: 'none',
        showIf: isV3,
        options: [
          { v: 'none', l: 'Yok (noAuthNoPriv)' },
          { v: 'md5', l: 'MD5' },
          { v: 'sha', l: 'SHA' },
          { v: 'sha224', l: 'SHA-224' },
          { v: 'sha256', l: 'SHA-256' },
          { v: 'sha384', l: 'SHA-384' },
          { v: 'sha512', l: 'SHA-512' },
        ],
      },
      {
        key: 'auth_password',
        label: 'Kimlik doğrulama şifresi',
        kind: 'secret',
        required: true,
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none',
      },
      {
        key: 'priv_protocol',
        label: 'Gizlilik protokolü',
        kind: 'select',
        def: 'none',
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none',
        options: [
          { v: 'none', l: 'Yok (authNoPriv)' },
          { v: 'des', l: 'DES' },
          { v: 'aes', l: 'AES-128' },
          { v: 'aes192', l: 'AES-192' },
          { v: 'aes256', l: 'AES-256' },
        ],
      },
      {
        key: 'priv_password',
        label: 'Gizlilik şifresi',
        kind: 'secret',
        required: true,
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none' && v.priv_protocol !== 'none',
      },
      {
        key: 'condition',
        label: 'Koşul',
        kind: 'select',
        def: '',
        advanced: true,
        section: 'Değer kontrolü',
        sectionHelp: 'Koşul seçilmezse değerin okunabilmesi yeterlidir.',
        options: [
          { v: '', l: 'Yok (değer okunabiliyorsa çalışıyor)' },
          { v: '==', l: 'eşittir (==)' },
          { v: '!=', l: 'eşit değildir (!=)' },
          { v: '>', l: 'büyüktür (>)' },
          { v: '>=', l: 'büyük veya eşit (>=)' },
          { v: '<', l: 'küçüktür (<)' },
          { v: '<=', l: 'küçük veya eşit (<=)' },
          { v: 'contains', l: 'içerir' },
        ],
      },
      {
        key: 'expected',
        label: 'Beklenen değer',
        kind: 'text',
        required: true,
        advanced: true,
        showIf: (v) => !!v.condition,
      },
    ],
  },

  // Veritabanı
  {
    key: 'mysql',
    label: 'MySQL / MariaDB',
    badge: 'MYSQL',
    desc: 'Veritabanına bağlanıp sorgu çalıştırır',
    icon: 'database',
    category: 'database',
    keywords: 'mariadb sql',
    fields: sqlFields(3306, {
      key: 'tls',
      label: 'TLS',
      kind: 'select',
      def: 'false',
      options: [
        { v: 'false', l: 'Kapalı' },
        { v: 'true', l: 'Açık, sertifika doğrulanır' },
        { v: 'skip-verify', l: 'Açık, sertifika doğrulanmaz' },
      ],
    }),
  },
  {
    key: 'postgres',
    label: 'PostgreSQL',
    badge: 'POSTGRES',
    desc: 'Veritabanına bağlanıp sorgu çalıştırır',
    icon: 'elephant',
    category: 'database',
    keywords: 'postgresql psql sql',
    fields: sqlFields(5432, {
      key: 'sslmode',
      label: 'SSL modu',
      kind: 'select',
      def: 'prefer',
      help: 'prefer: mümkünse TLS; verify-full: sertifika ve sunucu adı tam doğrulanır',
      options: [
        { v: 'prefer', l: 'prefer' },
        { v: 'disable', l: 'disable' },
        { v: 'require', l: 'require' },
        { v: 'verify-full', l: 'verify-full' },
      ],
    }),
  },
  {
    key: 'mssql',
    label: 'Microsoft SQL Server',
    badge: 'MSSQL',
    desc: 'Veritabanına bağlanıp sorgu çalıştırır',
    icon: 'table',
    category: 'database',
    keywords: 'sql server azure',
    fields: sqlFields(1433, {
      key: 'encrypt',
      label: 'Bağlantıyı şifrele',
      kind: 'bool',
      help: 'Açıksa bağlantı şifrelenir; sunucu sertifikası doğrulanmaz (iç ağdaki kendinden imzalı sertifikalar için)',
    }),
  },
  {
    key: 'redis',
    label: 'Redis',
    badge: 'REDIS',
    desc: 'PING gönderir, isteğe bağlı anahtar kontrolü yapar',
    icon: 'memory',
    category: 'database',
    keywords: 'valkey keydb cache',
    fields: [
      host('redis.ornek.com'),
      port(6379),
      { key: 'password', label: 'Şifre', kind: 'secret', optional: true },
      { key: 'tls', label: 'TLS kullan', kind: 'bool' },
      {
        key: 'username',
        label: 'Kullanıcı adı (ACL, isteğe bağlı)',
        kind: 'text',
        advanced: true,
        section: 'Bağlantı',
      },
      {
        key: 'db',
        label: 'Veritabanı indeksi',
        kind: 'number',
        def: 0,
        min: 0,
        max: 15,
        pattern: /^\d{1,2}$/,
        patternMsg: 'Veritabanı indeksi 0-15 arasında olmalı.',
        advanced: true,
      },
      {
        key: 'key',
        label: 'Anahtar',
        kind: 'text',
        optional: true,
        mono: true,
        advanced: true,
        section: 'Anahtar kontrolü',
        sectionHelp: 'Anahtar boşsa sadece PING gönderilir.',
      },
      {
        key: 'expected',
        label: 'Beklenen değer',
        kind: 'text',
        optional: true,
        advanced: true,
        showIf: (v) => !!v.key?.trim(),
        help: 'Boşsa anahtarın var olması yeterli.',
      },
    ],
  },
  {
    key: 'mongodb',
    label: 'MongoDB',
    badge: 'MONGO',
    desc: 'Bağlanıp ping komutu çalıştırır',
    icon: 'leaf',
    category: 'database',
    keywords: 'mongo nosql atlas',
    fields: [
      {
        key: 'uri',
        label: 'Bağlantı URI’si',
        kind: 'secret',
        placeholder: 'mongodb://kullanici:sifre@host:27017',
        required: true,
        wide: true,
        pattern: /^mongodb(\+srv)?:\/\/\S+$/,
        patternMsg: 'URI mongodb:// veya mongodb+srv:// ile başlamalı.',
        help: 'Kimlik bilgisi içerir; kaydedildikten sonra gizlenir. mongodb+srv:// da desteklenir',
      },
      { key: 'database', label: 'Veritabanı', kind: 'text', placeholder: 'admin', advanced: true, help: 'Boşsa admin.' },
    ],
  },

  // Sistem ve sinyaller
  {
    key: 'docker',
    label: 'Docker konteyner',
    badge: 'DOCKER',
    desc: 'Konteynerin çalışıp sağlıklı olduğunu kontrol eder',
    icon: 'box',
    category: 'system',
    keywords: 'container konteyner compose',
    fields: [
      {
        key: 'container',
        label: 'Konteyner adı veya ID',
        kind: 'text',
        placeholder: 'web-1',
        required: true,
        mono: true,
        wide: true,
        suggestName: true,
      },
      {
        key: 'endpoint',
        label: 'Docker API adresi',
        kind: 'text',
        def: 'unix:///var/run/docker.sock',
        mono: true,
        wide: true,
        help: 'unix:///var/run/docker.sock, tcp://host:2375 veya http(s)://… (ör. docker-socket-proxy). Güvenlik: docker.sock’u salt okunur (:ro) bağlayın ya da yalnızca CONTAINERS=1 izinli tecnativa/docker-socket-proxy kullanın.',
      },
    ],
  },
  {
    key: 'push',
    label: 'Push',
    badge: 'PUSH',
    desc: 'Cron işlerinin düzenli sinyal göndermesini bekler',
    icon: 'inbox',
    category: 'system',
    keywords: 'heartbeat cron sinyal',
    timeout: false,
    remote: false,
  },
  {
    key: 'group',
    label: 'Grup',
    badge: 'GRUP',
    desc: 'Seçtiğiniz monitörlerin durumunu tek monitörde toplar',
    icon: 'layers',
    category: 'system',
    timeout: false,
    upsideDown: false,
    remote: false,
  },
];

/** HTTP monitörünün gelişmiş bölümündeki ek alanlar (proxy, mTLS, OAuth2). */
export const HTTP_EXTRA_FIELDS: CfgField[] = [
  {
    key: 'proxy_url',
    label: 'Proxy adresi',
    kind: 'text',
    placeholder: 'socks5://10.0.0.5:1080',
    mono: true,
    wide: true,
    section: 'Proxy',
    help: 'http://, https://, socks5:// veya socks5h:// — socks5h: alan adı proxy üzerinde çözülür',
    pattern: /^(https?|socks5h?):\/\/\S+$/i,
    patternMsg: 'Proxy adresi http://, https://, socks5:// veya socks5h:// ile başlamalı.',
  },
  { key: 'proxy_user', label: 'Proxy kullanıcı adı', kind: 'text', showIf: (v) => !!v.proxy_url?.trim() },
  { key: 'proxy_pass', label: 'Proxy şifresi', kind: 'secret', showIf: (v) => !!v.proxy_url?.trim() },
  {
    key: 'tls_cert',
    label: 'İstemci sertifikası (PEM)',
    kind: 'pem',
    placeholder: '-----BEGIN CERTIFICATE-----',
    section: 'İstemci sertifikası (mTLS)',
    sectionHelp: 'Sunucu istemci sertifikası istiyorsa sertifika ve özel anahtarı birlikte girin.',
  },
  {
    key: 'tls_key',
    label: 'Özel anahtar (PEM)',
    kind: 'pem',
    secret: true,
    placeholder: '-----BEGIN PRIVATE KEY-----',
  },
  {
    key: 'tls_ca',
    label: 'Özel kök sertifika (CA)',
    kind: 'pem',
    placeholder: '-----BEGIN CERTIFICATE-----',
    help: 'Sistem kök sertifikalarına ek olarak güvenilir',
  },
  {
    key: 'oauth_token_url',
    label: 'Token adresi',
    kind: 'url',
    placeholder: 'https://auth.ornek.com/oauth/token',
    wide: true,
    section: 'OAuth2 (istemci kimlik bilgileri)',
    sectionHelp: 'Doluysa her istekten önce bu adresten erişim token’ı alınır ve Authorization: Bearer başlığıyla gönderilir. Basic auth ile birlikte kullanılamaz.',
  },
  { key: 'oauth_client_id', label: 'İstemci kimliği (Client ID)', kind: 'text', showIf: (v) => !!v.oauth_token_url?.trim() },
  { key: 'oauth_client_secret', label: 'İstemci sırrı (Client Secret)', kind: 'secret', showIf: (v) => !!v.oauth_token_url?.trim() },
  {
    key: 'oauth_scopes',
    label: 'Kapsamlar (scope)',
    kind: 'text',
    placeholder: 'okuma yazma',
    optional: true,
    showIf: (v) => !!v.oauth_token_url?.trim(),
  },
  {
    key: 'oauth_auth_style',
    label: 'Kimlik gönderimi',
    kind: 'select',
    def: 'header',
    showIf: (v) => !!v.oauth_token_url?.trim(),
    options: [
      { v: 'header', l: 'Authorization başlığında' },
      { v: 'body', l: 'Form gövdesinde' },
    ],
  },
];

const BY_KEY = new Map<string, MonitorTypeDef>(MONITOR_TYPES.map((t) => [t.key, t]));

export function typeDef(key: string): MonitorTypeDef | undefined {
  return BY_KEY.get(key);
}

/** Rozet metni; bilinmeyen (sonradan eklenmiş) tiplerde tip adının büyük harfi. */
export function typeLabel(key: string): string {
  return BY_KEY.get(key)?.badge ?? key.toLocaleUpperCase('tr');
}

/** Tip seçicideki tam ad. */
export function typeName(key: string): string {
  return BY_KEY.get(key)?.label ?? key;
}

export const hasTimeout = (key: string) => BY_KEY.get(key)?.timeout !== false;
export const hasUpsideDown = (key: string) => BY_KEY.get(key)?.upsideDown !== false;
/** Kontrol noktalarında çalıştırılabilir mi (push ve grup hayır). */
export const isRemoteCapable = (key: string) => BY_KEY.get(key)?.remote !== false;

/** Adresi tarayıcıda açılabilen tipler (hedef bağlantı olarak gösterilir). */
export const isWebTarget = (key: string) => key === 'http' || key === 'browser';

export const GROUP_MODES: { v: 'any_down' | 'all_down'; l: string }[] = [
  { v: 'any_down', l: 'Herhangi biri çalışmıyorsa DOWN' },
  { v: 'all_down', l: 'Hepsi çalışmıyorsa DOWN' },
];

/**
 * Hedef metni gösterim için: sunucu bazı tiplerde (ör. MongoDB) hedefi maskeli
 * ayardan ürettiğinde maske URL-kodlu gelir; okunaklı maskeye çevrilir.
 */
export const displayTarget = (t: string) => t.replace(/(%E2%80%A2)+/gi, '••••••');
