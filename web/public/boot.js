// İlk boyama: topoloji adresi doğrudan açılınca uygulama paketi yüklenmeden iskelet görünür.
// Satır içi betik güvenlik politikasına takıldığı için ayrı dosya; App ilk durumda kaldırır.
try {
  var r = document.documentElement;
  if (location.hash.indexOf('#/topology') === 0) r.classList.add('boot-topo');
  if (localStorage.getItem('uptime.theme') === 'light') r.setAttribute('data-theme', 'light');
} catch (e) {}
