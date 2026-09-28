// Bildirimler sayfası (pages/Notifications.svelte) ve kanal formu (components/NotificationForm.svelte).
export default {
  list: {
    title: 'Bildirimler',
    newChannel: 'Yeni kanal',
    loadFailed: 'Kanallar yüklenemedi',
    emptyTitle: 'Henüz bildirim kanalı yok',
    emptyText: 'Bir monitör çalışmadığında haberdar olmak için WhatsApp, Telegram, e-posta veya başka bir kanal ekleyin.',
    addChannel: 'Kanal ekle',
    default: 'Varsayılan',
    disabled: 'Devre dışı',
    monitorCount: '{count} monitör',
    footHelp:
      '“Varsayılan” kanallar yeni eklenen monitörlerde otomatik seçili gelir. Her monitörün kanallarını monitör düzenleme sayfasından değiştirebilirsiniz.',
    added: '“{name}” eklendi',
    saved: 'Kanal kaydedildi',
    deletedNamed: '“{name}” silindi',
    deleted: 'Kanal silindi',
  },
  form: {
    titleEdit: 'Bildirim kanalını düzenle',
    titleNew: 'Yeni bildirim kanalı',
    type: 'Tip',
    namePlaceholder: 'Ör. {type} — Ekip',
    nameRequired: 'Kanal adı gerekli.',
    exampleSummary: 'Gönderilen JSON örneği',
    /** {event}, {down} … yer tutucuları <code> olarak çizilir (adları olduğu gibi). */
    exampleHelp:
      '{event}: {down}, {up}, {reminder}, {cert} veya {test}. {downtime_seconds} düzelme bildiriminde, {cert_days} SSL uyarısında gelir.',
    active: 'Etkin',
    activeHelp: 'Devre dışı kanallara bildirim gönderilmez.',
    isDefault: 'Yeni monitörlere varsayılan olarak ekle',
    applyExisting: 'Mevcut tüm monitörlere ekle',
    applyExistingHelp: 'Kaydettiğinizde bu kanal şu anki tüm monitörlere bağlanır.',
    test: 'Test gönder',
    testSent: 'Test bildirimi gönderildi. Kanalınızı kontrol edin.',
    /** {dest}: "Adres ve port" gibi değişen hedef; {fields}: yeniden girilecek alanlar. */
    rebind: '{dest} değiştiği için kayıtlı {fields} güvenlik gereği yeni hedefe taşınmaz; kaydetmeden önce yeniden girin.',
    deleteTitle: 'Kanalı sil',
    deleteMessage: '“{name}” bildirim kanalı silinecek ve bağlı olduğu monitörlerden kaldırılacak.',
  },
};
