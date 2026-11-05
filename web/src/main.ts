import './app.css';

// 1,3 MB'lık paket ana iş parçacığını yarım saniye tutar: önce iskelet boyansın (public/boot.js),
// paket ayrı parça olarak sonra çalışsın. Gizli sekmede kare gelmez, doğrudan başlar.
const start = () => import('./start');
if (document.hidden) void start();
else requestAnimationFrame(() => setTimeout(start, 0));
