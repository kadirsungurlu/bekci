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
//
// Dil: metinler (ad, açıklama, alan etiketi/yardımı, seçenek adları) getter'dır;
// her okumada geçerli dilde döner (lib/i18n/{tr,en}/monitorTypes.ts). Bu nesneleri
// yaymayın ({ ...alan }) — yayma getter'ı o anki dilde dondurur. Teknik adlar
// (HTTP(S), Ping, PostgreSQL, OID …) düz metin olarak kalır.

import type { MonitorType } from './api';
import type { Field, FieldKind } from './notifyTypes';
import type { IconName } from '../components/Icon.svelte';
import { t, type TKey } from './i18n';

export type TypeCategory = 'web' | 'network' | 'database' | 'system';

export const CATEGORY_LABELS: Record<TypeCategory, string> = {
  get web() {
    return t('monitorTypes.categories.web');
  },
  get network() {
    return t('monitorTypes.categories.network');
  },
  get database() {
    return t('monitorTypes.categories.database');
  },
  get system() {
    return t('monitorTypes.categories.system');
  },
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
  /** Arama için ek anahtar kelimeler (iki dilde; gösterilmez). */
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

/** Çevrilen seçenek: { v, l } — l her okumada geçerli dilde. */
const opt = (v: string, key: TKey) => ({
  v,
  get l() {
    return t(key);
  },
});

const host = (ph: () => string = () => t('monitorTypes.ph.dbHost')): CfgField => ({
  key: 'host',
  get label() {
    return t('monitorTypes.f.host');
  },
  kind: 'text',
  get placeholder() {
    return ph();
  },
  required: true,
  suggestName: true,
});

/** Port alanı; help sözlük anahtarıdır. */
const port = (def: number, o: { required?: boolean; help?: TKey } = {}): CfgField => ({
  key: 'port',
  get label() {
    return t('monitorTypes.f.port');
  },
  kind: 'number',
  def,
  min: 1,
  max: 65535,
  pattern: PORT_RE,
  get patternMsg() {
    return t('monitorTypes.f.portRange');
  },
  required: o.required,
  get help() {
    return o.help ? t(o.help) : undefined;
  },
});

const ignoreTls = (o: { help?: TKey; showIf?: CfgField['showIf'] } = {}): CfgField => ({
  key: 'ignore_tls',
  get label() {
    return t('monitorTypes.f.ignoreTls');
  },
  kind: 'bool',
  get help() {
    return t(o.help ?? 'monitorTypes.f.ignoreTlsHelp');
  },
  advanced: true,
  showIf: o.showIf,
});

interface CredOpts {
  optional?: boolean;
  required?: boolean;
  showIf?: CfgField['showIf'];
  placeholder?: TKey;
  section?: TKey;
}

const username = (o: CredOpts = {}): CfgField => ({
  key: 'username',
  get label() {
    return t('common.username');
  },
  kind: 'text',
  optional: o.optional,
  required: o.required,
  showIf: o.showIf,
  get placeholder() {
    return o.placeholder ? t(o.placeholder) : undefined;
  },
  get section() {
    return o.section ? t(o.section) : undefined;
  },
});

const password = (o: CredOpts = {}): CfgField => ({
  key: 'password',
  get label() {
    return t('common.password');
  },
  kind: 'secret',
  optional: o.optional,
});

/** MySQL, PostgreSQL ve MSSQL'in ortak alanları; tls alanı `advanced: true` ile verilir. */
function sqlFields(def: number, tls: CfgField): CfgField[] {
  return [
    host(),
    port(def),
    username({ placeholder: 'monitorTypes.ph.dbUser' }),
    password(),
    {
      key: 'database',
      get label() {
        return t('monitorTypes.f.database');
      },
      kind: 'text',
      optional: true,
      wide: true,
    },
    {
      key: 'query',
      get label() {
        return t('monitorTypes.f.query');
      },
      kind: 'text',
      def: 'SELECT 1',
      mono: true,
      wide: true,
      advanced: true,
    },
    {
      key: 'expected',
      get label() {
        return t('monitorTypes.f.expectedResult');
      },
      kind: 'text',
      optional: true,
      advanced: true,
      get help() {
        return t('monitorTypes.f.expectedResultHelp');
      },
    },
    tls,
  ];
}

/** Karşılaştırma işleçleri (MQTT JSON, SNMP, HTTP JSON). */
const cmpOps = () => [
  opt('==', 'monitorTypes.ops.eq'),
  opt('!=', 'monitorTypes.ops.ne'),
  opt('contains', 'monitorTypes.ops.contains'),
  opt('>', 'monitorTypes.ops.gt'),
  opt('>=', 'monitorTypes.ops.ge'),
  opt('<', 'monitorTypes.ops.lt'),
  opt('<=', 'monitorTypes.ops.le'),
];

/** HTTP JSON kontrolünün koşulları (formdaki seçici). */
export const JSON_OPS: { v: string; l: string }[] = [...cmpOps(), opt('exists', 'monitorTypes.ops.exists')];

const hasTopic = (v: Record<string, string>) => !!v.topic?.trim();
const isV3 = (v: Record<string, string>) => v.version === 'v3';

// Kayıt defteri ---------------------------------------------------------------------------

export const MONITOR_TYPES: MonitorTypeDef[] = [
  // Web
  {
    key: 'http',
    label: 'HTTP(S)',
    badge: 'HTTP',
    get desc() {
      return t('monitorTypes.types.http.desc');
    },
    icon: 'globe',
    category: 'web',
    keywords: 'web site api url https keyword json',
  },

  // Ağ ve protokoller
  {
    key: 'tcp',
    label: 'TCP Port',
    badge: 'TCP',
    get desc() {
      return t('monitorTypes.types.tcp.desc');
    },
    icon: 'plug',
    category: 'network',
  },
  {
    key: 'ping',
    label: 'Ping',
    badge: 'PING',
    get desc() {
      return t('monitorTypes.types.ping.desc');
    },
    icon: 'radio',
    category: 'network',
    keywords: 'icmp',
  },
  {
    key: 'dns',
    label: 'DNS',
    badge: 'DNS',
    get desc() {
      return t('monitorTypes.types.dns.desc');
    },
    icon: 'server',
    category: 'network',
  },
  {
    key: 'tlscert',
    get label() {
      return t('monitorTypes.types.tlscert.label');
    },
    badge: 'TLS',
    get desc() {
      return t('monitorTypes.types.tlscert.desc');
    },
    get about() {
      return t('monitorTypes.types.tlscert.about');
    },
    icon: 'certificate',
    category: 'network',
    keywords: 'ssl sertifika certificate imap ldaps',
    fields: [
      host(() => t('monitorTypes.ph.mailHost')),
      port(443, { required: true, help: 'monitorTypes.f.tlscert.portHelp' }),
      {
        key: 'server_name',
        get label() {
          return t('monitorTypes.f.tlscert.sni');
        },
        kind: 'text',
        optional: true,
        advanced: true,
        get help() {
          return t('monitorTypes.f.tlscert.sniHelp');
        },
      },
      ignoreTls({ help: 'monitorTypes.f.tlscert.ignoreHelp' }),
    ],
  },
  {
    key: 'smtp',
    get label() {
      return t('monitorTypes.types.smtp.label');
    },
    badge: 'SMTP',
    get desc() {
      return t('monitorTypes.types.smtp.desc');
    },
    get about() {
      return t('monitorTypes.types.smtp.about');
    },
    icon: 'mail',
    category: 'network',
    keywords: 'e-posta mail eposta email starttls',
    fields: [
      host(() => t('monitorTypes.ph.mailHost')),
      port(25, { help: 'monitorTypes.f.smtp.portHelp' }),
      {
        key: 'security',
        get label() {
          return t('monitorTypes.f.smtp.security');
        },
        kind: 'select',
        def: 'none',
        wide: true,
        options: [
          opt('none', 'monitorTypes.f.smtp.secNone'),
          { v: 'starttls', l: 'STARTTLS' },
          opt('tls', 'monitorTypes.f.smtp.secTls'),
        ],
      },
      ignoreTls({ showIf: (v) => v.security !== 'none' }),
      {
        key: 'expected_banner',
        get label() {
          return t('monitorTypes.f.smtp.banner');
        },
        kind: 'text',
        optional: true,
        advanced: true,
        placeholder: 'ESMTP',
        get help() {
          return t('monitorTypes.f.smtp.bannerHelp');
        },
      },
    ],
  },
  {
    key: 'websocket',
    label: 'WebSocket',
    badge: 'WS',
    get desc() {
      return t('monitorTypes.types.websocket.desc');
    },
    get about() {
      return t('monitorTypes.types.websocket.about');
    },
    icon: 'arrows-lr',
    category: 'network',
    keywords: 'ws wss socket',
    fields: [
      {
        key: 'url',
        get label() {
          return t('monitorTypes.f.ws.url');
        },
        kind: 'text',
        get placeholder() {
          return t('monitorTypes.ph.wsUrl');
        },
        required: true,
        wide: true,
        mono: true,
        suggestName: true,
        pattern: /^wss?:\/\/\S+$/i,
        get patternMsg() {
          return t('monitorTypes.f.ws.urlMsg');
        },
      },
      {
        key: 'send',
        get label() {
          return t('monitorTypes.f.ws.send');
        },
        kind: 'text',
        optional: true,
        wide: true,
        mono: true,
        placeholder: '{"type":"ping"}',
        advanced: true,
        get section() {
          return t('monitorTypes.f.ws.message');
        },
      },
      {
        key: 'keyword',
        get label() {
          return t('monitorTypes.f.expectedKeyword');
        },
        kind: 'text',
        optional: true,
        wide: true,
        advanced: true,
        get help() {
          return t('monitorTypes.f.ws.keywordHelp');
        },
      },
      {
        key: 'headers',
        get label() {
          return t('monitorTypes.f.headers');
        },
        kind: 'textarea',
        secret: true,
        optional: true,
        placeholder: 'Authorization: Bearer abc123',
        advanced: true,
        get help() {
          return t('monitorTypes.f.headerPerLine');
        },
      },
      ignoreTls(),
    ],
  },
  {
    key: 'grpc',
    get label() {
      return t('monitorTypes.types.grpc.label');
    },
    badge: 'GRPC',
    get desc() {
      return t('monitorTypes.types.grpc.desc');
    },
    get about() {
      return t('monitorTypes.types.grpc.about');
    },
    icon: 'cpu',
    category: 'network',
    keywords: 'grpc health protobuf',
    fields: [
      {
        key: 'target',
        get label() {
          return t('monitorTypes.f.host');
        },
        kind: 'text',
        get placeholder() {
          return t('monitorTypes.ph.grpcTarget');
        },
        required: true,
        mono: true,
        suggestName: true,
        pattern: /^\S+:\d{1,5}$/,
        get patternMsg() {
          return t('monitorTypes.f.grpc.targetMsg');
        },
      },
      {
        key: 'service',
        get label() {
          return t('monitorTypes.f.grpc.service');
        },
        kind: 'text',
        optional: true,
        mono: true,
        get help() {
          return t('monitorTypes.f.grpc.serviceHelp');
        },
      },
      {
        key: 'tls',
        get label() {
          return t('monitorTypes.f.useTls');
        },
        kind: 'bool',
      },
      ignoreTls({ showIf: (v) => v.tls === 'true' }),
      {
        key: 'metadata',
        get label() {
          return t('monitorTypes.f.grpc.metadata');
        },
        kind: 'textarea',
        secret: true,
        optional: true,
        placeholder: 'authorization: Bearer abc123',
        advanced: true,
        get help() {
          return t('monitorTypes.f.headerPerLine');
        },
      },
    ],
  },
  {
    key: 'mqtt',
    label: 'MQTT',
    badge: 'MQTT',
    get desc() {
      return t('monitorTypes.types.mqtt.desc');
    },
    get about() {
      return t('monitorTypes.types.mqtt.about');
    },
    icon: 'rss',
    category: 'network',
    keywords: 'mosquitto iot broker',
    fields: [
      {
        key: 'broker_url',
        get label() {
          return t('monitorTypes.f.mqtt.broker');
        },
        kind: 'text',
        get placeholder() {
          return t('monitorTypes.ph.mqttBroker');
        },
        required: true,
        wide: true,
        mono: true,
        suggestName: true,
        // Sunucu yalnızca bu şemaları kabul eder (mqtt:// ve mqtts:// değil); adreste sunucu adı olmalı.
        pattern: /^(tcp|ssl|tls|ws|wss):\/\/[^\s/?#]+\S*$/i,
        get patternMsg() {
          return t('monitorTypes.f.mqtt.brokerMsg');
        },
        get help() {
          return t('monitorTypes.f.mqtt.brokerHelp');
        },
      },
      username({ optional: true }),
      password({ optional: true }),
      {
        key: 'topic',
        get label() {
          return t('monitorTypes.f.mqtt.topic');
        },
        kind: 'text',
        optional: true,
        wide: true,
        mono: true,
        get placeholder() {
          return t('monitorTypes.ph.mqttTopic');
        },
        advanced: true,
        get section() {
          return t('monitorTypes.f.mqtt.section');
        },
        get sectionHelp() {
          return t('monitorTypes.f.mqtt.sectionHelp');
        },
      },
      {
        key: 'keyword',
        get label() {
          return t('monitorTypes.f.expectedKeyword');
        },
        kind: 'text',
        optional: true,
        wide: true,
        advanced: true,
        showIf: hasTopic,
        get help() {
          return t('monitorTypes.f.mqtt.keywordHelp');
        },
      },
      {
        key: 'json_path',
        get label() {
          return t('monitorTypes.f.mqtt.jsonPath');
        },
        kind: 'text',
        optional: true,
        mono: true,
        placeholder: 'data.status',
        advanced: true,
        showIf: hasTopic,
      },
      {
        key: 'json_op',
        get label() {
          return t('monitorTypes.f.mqtt.compare');
        },
        kind: 'select',
        def: '==',
        options: cmpOps(),
        advanced: true,
        showIf: (v) => hasTopic(v) && !!v.json_path?.trim(),
      },
      {
        key: 'json_expected',
        get label() {
          return t('monitorTypes.f.expectedValue');
        },
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
    get desc() {
      return t('monitorTypes.types.snmp.desc');
    },
    get about() {
      return t('monitorTypes.types.snmp.about');
    },
    icon: 'router',
    category: 'network',
    keywords: 'switch router oid mib',
    fields: [
      host(() => '10.0.0.1'),
      port(161),
      {
        key: 'version',
        get label() {
          return t('monitorTypes.f.snmp.version');
        },
        kind: 'select',
        def: 'v2c',
        options: [{ v: 'v1', l: 'v1' }, { v: 'v2c', l: 'v2c' }, opt('v3', 'monitorTypes.f.snmp.v3')],
      },
      {
        key: 'community',
        label: 'Community',
        kind: 'secret',
        placeholder: 'public',
        get help() {
          return t('monitorTypes.f.snmp.communityHelp');
        },
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
        get patternMsg() {
          return t('monitorTypes.f.snmp.oidMsg');
        },
        wide: true,
      },
      username({ required: true, showIf: isV3, section: 'monitorTypes.f.snmp.v3Section' }),
      {
        key: 'auth_protocol',
        get label() {
          return t('monitorTypes.f.snmp.authProtocol');
        },
        kind: 'select',
        def: 'none',
        showIf: isV3,
        options: [
          {
            v: 'none',
            get l() {
              return `${t('common.none')} (noAuthNoPriv)`;
            },
          },
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
        get label() {
          return t('monitorTypes.f.snmp.authPassword');
        },
        kind: 'secret',
        required: true,
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none',
      },
      {
        key: 'priv_protocol',
        get label() {
          return t('monitorTypes.f.snmp.privProtocol');
        },
        kind: 'select',
        def: 'none',
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none',
        options: [
          {
            v: 'none',
            get l() {
              return `${t('common.none')} (authNoPriv)`;
            },
          },
          { v: 'des', l: 'DES' },
          { v: 'aes', l: 'AES-128' },
          { v: 'aes192', l: 'AES-192' },
          { v: 'aes256', l: 'AES-256' },
        ],
      },
      {
        key: 'priv_password',
        get label() {
          return t('monitorTypes.f.snmp.privPassword');
        },
        kind: 'secret',
        required: true,
        showIf: (v) => isV3(v) && v.auth_protocol !== 'none' && v.priv_protocol !== 'none',
      },
      {
        key: 'condition',
        get label() {
          return t('monitorTypes.f.snmp.condition');
        },
        kind: 'select',
        def: '',
        advanced: true,
        get section() {
          return t('monitorTypes.f.snmp.valueSection');
        },
        get sectionHelp() {
          return t('monitorTypes.f.snmp.valueSectionHelp');
        },
        options: [
          opt('', 'monitorTypes.f.snmp.condNone'),
          ...cmpOps().filter((o) => o.v !== 'contains'),
          opt('contains', 'monitorTypes.ops.contains'),
        ],
      },
      {
        key: 'expected',
        get label() {
          return t('monitorTypes.f.expectedValue');
        },
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
    get desc() {
      return t('monitorTypes.types.sql.desc');
    },
    icon: 'database',
    category: 'database',
    keywords: 'mariadb sql',
    fields: sqlFields(3306, {
      key: 'tls',
      label: 'TLS',
      kind: 'select',
      def: 'false',
      advanced: true,
      options: [
        opt('false', 'common.off'),
        opt('true', 'monitorTypes.f.mysql.tlsTrue'),
        opt('skip-verify', 'monitorTypes.f.mysql.tlsSkip'),
      ],
    }),
  },
  {
    key: 'postgres',
    label: 'PostgreSQL',
    badge: 'POSTGRES',
    get desc() {
      return t('monitorTypes.types.sql.desc');
    },
    icon: 'elephant',
    category: 'database',
    keywords: 'postgresql psql sql',
    fields: sqlFields(5432, {
      key: 'sslmode',
      get label() {
        return t('monitorTypes.f.postgres.sslmode');
      },
      kind: 'select',
      def: 'prefer',
      advanced: true,
      get help() {
        return t('monitorTypes.f.postgres.sslmodeHelp');
      },
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
    get desc() {
      return t('monitorTypes.types.sql.desc');
    },
    icon: 'table',
    category: 'database',
    keywords: 'sql server azure',
    fields: sqlFields(1433, {
      key: 'encrypt',
      get label() {
        return t('monitorTypes.f.mssql.encrypt');
      },
      kind: 'bool',
      advanced: true,
      get help() {
        return t('monitorTypes.f.mssql.encryptHelp');
      },
    }),
  },
  {
    key: 'redis',
    label: 'Redis',
    badge: 'REDIS',
    get desc() {
      return t('monitorTypes.types.redis.desc');
    },
    icon: 'memory',
    category: 'database',
    keywords: 'valkey keydb cache',
    fields: [
      host(() => t('monitorTypes.ph.redisHost')),
      port(6379),
      password({ optional: true }),
      {
        key: 'tls',
        get label() {
          return t('monitorTypes.f.useTls');
        },
        kind: 'bool',
      },
      {
        key: 'username',
        get label() {
          return t('monitorTypes.f.redis.username');
        },
        kind: 'text',
        advanced: true,
        get section() {
          return t('monitorTypes.f.redis.connection');
        },
      },
      {
        key: 'db',
        get label() {
          return t('monitorTypes.f.redis.db');
        },
        kind: 'number',
        def: 0,
        min: 0,
        max: 15,
        pattern: /^\d{1,2}$/,
        get patternMsg() {
          return t('monitorTypes.f.redis.dbMsg');
        },
        advanced: true,
      },
      {
        key: 'key',
        get label() {
          return t('monitorTypes.f.redis.key');
        },
        kind: 'text',
        optional: true,
        mono: true,
        advanced: true,
        get section() {
          return t('monitorTypes.f.redis.keySection');
        },
        get sectionHelp() {
          return t('monitorTypes.f.redis.keySectionHelp');
        },
      },
      {
        key: 'expected',
        get label() {
          return t('monitorTypes.f.expectedValue');
        },
        kind: 'text',
        optional: true,
        advanced: true,
        showIf: (v) => !!v.key?.trim(),
        get help() {
          return t('monitorTypes.f.redis.expectedHelp');
        },
      },
    ],
  },
  {
    key: 'mongodb',
    label: 'MongoDB',
    badge: 'MONGO',
    get desc() {
      return t('monitorTypes.types.mongodb.desc');
    },
    icon: 'leaf',
    category: 'database',
    keywords: 'mongo nosql atlas',
    fields: [
      {
        key: 'uri',
        get label() {
          return t('monitorTypes.f.mongo.uri');
        },
        kind: 'secret',
        get placeholder() {
          return t('monitorTypes.ph.mongoUri');
        },
        required: true,
        wide: true,
        pattern: /^mongodb(\+srv)?:\/\/\S+$/,
        get patternMsg() {
          return t('monitorTypes.f.mongo.uriMsg');
        },
        get help() {
          return t('monitorTypes.f.mongo.uriHelp');
        },
      },
      {
        key: 'database',
        get label() {
          return t('monitorTypes.f.database');
        },
        kind: 'text',
        placeholder: 'admin',
        advanced: true,
        get help() {
          return t('monitorTypes.f.mongo.dbHelp');
        },
      },
    ],
  },

  // Sistem ve sinyaller
  {
    key: 'docker',
    get label() {
      return t('monitorTypes.types.docker.label');
    },
    badge: 'DOCKER',
    get desc() {
      return t('monitorTypes.types.docker.desc');
    },
    icon: 'box',
    category: 'system',
    keywords: 'container konteyner compose',
    fields: [
      {
        key: 'container',
        get label() {
          return t('monitorTypes.f.docker.container');
        },
        kind: 'text',
        placeholder: 'web-1',
        required: true,
        mono: true,
        wide: true,
        suggestName: true,
      },
      {
        key: 'endpoint',
        get label() {
          return t('monitorTypes.f.docker.endpoint');
        },
        kind: 'text',
        def: 'unix:///var/run/docker.sock',
        mono: true,
        wide: true,
        get help() {
          return t('monitorTypes.f.docker.endpointHelp');
        },
      },
    ],
  },
  {
    key: 'push',
    label: 'Push',
    badge: 'PUSH',
    get desc() {
      return t('monitorTypes.types.push.desc');
    },
    icon: 'inbox',
    category: 'system',
    keywords: 'heartbeat cron sinyal',
    timeout: false,
    remote: false,
  },
  {
    key: 'group',
    get label() {
      return t('monitorTypes.types.group.label');
    },
    get badge() {
      return t('monitorTypes.types.group.badge');
    },
    get desc() {
      return t('monitorTypes.types.group.desc');
    },
    icon: 'layers',
    category: 'system',
    keywords: 'group grup',
    timeout: false,
    upsideDown: false,
    remote: false,
  },
];

const whenProxy = (v: Record<string, string>) => !!v.proxy_url?.trim();
const whenOAuth = (v: Record<string, string>) => !!v.oauth_token_url?.trim();

/** HTTP monitörünün gelişmiş bölümündeki ek alanlar (proxy, mTLS, OAuth2). */
export const HTTP_EXTRA_FIELDS: CfgField[] = [
  {
    key: 'proxy_url',
    get label() {
      return t('monitorTypes.f.http.proxyUrl');
    },
    kind: 'text',
    placeholder: 'socks5://10.0.0.5:1080',
    mono: true,
    wide: true,
    section: 'Proxy',
    get help() {
      return t('monitorTypes.f.http.proxyHelp');
    },
    pattern: /^(https?|socks5h?):\/\/\S+$/i,
    get patternMsg() {
      return t('monitorTypes.f.http.proxyMsg');
    },
  },
  {
    key: 'proxy_user',
    get label() {
      return t('monitorTypes.f.http.proxyUser');
    },
    kind: 'text',
    showIf: whenProxy,
  },
  {
    key: 'proxy_pass',
    get label() {
      return t('monitorTypes.f.http.proxyPass');
    },
    kind: 'secret',
    showIf: whenProxy,
  },
  {
    key: 'tls_cert',
    get label() {
      return t('monitorTypes.f.http.tlsCert');
    },
    kind: 'pem',
    placeholder: '-----BEGIN CERTIFICATE-----',
    get section() {
      return t('monitorTypes.f.http.mtlsSection');
    },
    get sectionHelp() {
      return t('monitorTypes.f.http.mtlsHelp');
    },
  },
  {
    key: 'tls_key',
    get label() {
      return t('monitorTypes.f.http.tlsKey');
    },
    kind: 'pem',
    secret: true,
    placeholder: '-----BEGIN PRIVATE KEY-----',
  },
  {
    key: 'tls_ca',
    get label() {
      return t('monitorTypes.f.http.tlsCa');
    },
    kind: 'pem',
    placeholder: '-----BEGIN CERTIFICATE-----',
    get help() {
      return t('monitorTypes.f.http.tlsCaHelp');
    },
  },
  {
    key: 'oauth_token_url',
    get label() {
      return t('monitorTypes.f.http.tokenUrl');
    },
    kind: 'url',
    get placeholder() {
      return t('monitorTypes.ph.oauthTokenUrl');
    },
    wide: true,
    get section() {
      return t('monitorTypes.f.http.oauthSection');
    },
    get sectionHelp() {
      return t('monitorTypes.f.http.oauthHelp');
    },
  },
  {
    key: 'oauth_client_id',
    get label() {
      return t('monitorTypes.f.http.clientId');
    },
    kind: 'text',
    showIf: whenOAuth,
  },
  {
    key: 'oauth_client_secret',
    get label() {
      return t('monitorTypes.f.http.clientSecret');
    },
    kind: 'secret',
    showIf: whenOAuth,
  },
  {
    key: 'oauth_scopes',
    get label() {
      return t('monitorTypes.f.http.scopes');
    },
    kind: 'text',
    get placeholder() {
      return t('monitorTypes.ph.oauthScopes');
    },
    optional: true,
    showIf: whenOAuth,
  },
  {
    key: 'oauth_auth_style',
    get label() {
      return t('monitorTypes.f.http.authStyle');
    },
    kind: 'select',
    def: 'header',
    showIf: whenOAuth,
    options: [opt('header', 'monitorTypes.f.http.authHeader'), opt('body', 'monitorTypes.f.http.authBody')],
  },
];

const BY_KEY = new Map<string, MonitorTypeDef>(MONITOR_TYPES.map((d) => [d.key, d]));

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
export const isWebTarget = (key: string) => key === 'http';

export const GROUP_MODES: { v: 'any_down' | 'all_down'; l: string }[] = [
  {
    v: 'any_down',
    get l() {
      return t('monitorTypes.groupModes.anyDown');
    },
  },
  {
    v: 'all_down',
    get l() {
      return t('monitorTypes.groupModes.allDown');
    },
  },
];

/**
 * Hedef metni gösterim için: sunucu bazı tiplerde (ör. MongoDB) hedefi maskeli
 * ayardan ürettiğinde maske URL-kodlu gelir; okunaklı maskeye çevrilir.
 */
export const displayTarget = (target: string) => target.replace(/(%E2%80%A2)+/gi, '••••••');

/**
 * Grup monitörünün hedefi: sunucu "3 monitör" diye saklar; geçerli dilde alt
 * monitör sayısı olarak yazılır (izleyicide ayar gizliyse hedefteki sayıdan).
 */
export function groupTarget(config: Record<string, unknown> | undefined, target: string): string {
  const ids = config?.monitor_ids;
  const n = Array.isArray(ids) ? ids.length : Number(/^(\d+)\b/.exec(target)?.[1] ?? NaN);
  return Number.isFinite(n) ? t('monitorTypes.childCount', { count: n }) : target;
}

/** Listede gösterilecek kısa hedef: web adreslerinde alan adı, diğerlerinde hedefin kendisi. */
export function shortTarget(type: string, target: string, config?: Record<string, unknown>): string {
  if (!target) return '';
  if (type === 'group') return groupTarget(config, target);
  if (isWebTarget(type)) {
    try {
      return new URL(target).host;
    } catch {
      /* ayrıştırılamazsa olduğu gibi */
    }
  }
  return displayTarget(target);
}
