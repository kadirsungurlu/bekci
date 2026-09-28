/**
 * Bir tr sözlüğünün İngilizce karşılığının biçimi: aynı anahtarlar, değerler
 * metin. en/<ad>.ts dosyaları `satisfies Shape<typeof tr>` ile denetlenir —
 * eksik veya fazla anahtar derleme (npm run check) hatasıdır.
 */
export type Shape<T> = { [K in keyof T]: T[K] extends string ? string : Shape<T[K]> };
