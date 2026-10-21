// Uçtan uca denemeler için sahte SMTP sunucusu (bağımlılıksız): her iletiyi
// bellekte tutar. Kimlik doğrulama ve TLS yok (kanal güvenliği "none").
//   const sink = await startSink(2525); await sink.waitFor((m) => ..., 20000)
import net from 'node:net';

export function startSink(port = 2525, host = '127.0.0.1') {
  const messages = [];
  const server = net.createServer((sock) => {
    sock.setEncoding('utf8');
    let buf = '';
    let inData = false;
    let msg = { from: '', to: [], data: '' };
    sock.write('220 sahte-smtp\r\n');
    sock.on('data', (chunk) => {
      buf += chunk;
      let i;
      while ((i = buf.indexOf('\r\n')) >= 0) {
        const line = buf.slice(0, i);
        buf = buf.slice(i + 2);
        if (inData) {
          if (line === '.') {
            inData = false;
            messages.push(parse(msg));
            msg = { from: '', to: [], data: '' };
            sock.write('250 tamam\r\n');
          } else {
            msg.data += (line.startsWith('..') ? line.slice(1) : line) + '\r\n';
          }
          continue;
        }
        const cmd = line.slice(0, 4).toUpperCase();
        if (cmd === 'EHLO' || cmd === 'HELO') sock.write('250-sahte-smtp\r\n250 8BITMIME\r\n');
        else if (cmd === 'MAIL') { msg.from = line; sock.write('250 tamam\r\n'); }
        else if (cmd === 'RCPT') { msg.to.push(line.replace(/^.*<|>.*$/g, '')); sock.write('250 tamam\r\n'); }
        else if (cmd === 'DATA') { inData = true; sock.write('354 devam\r\n'); }
        else if (cmd === 'QUIT') { sock.write('221 hoşça kal\r\n'); sock.end(); }
        else sock.write('250 tamam\r\n');
      }
    });
    sock.on('error', () => {});
  });
  return new Promise((resolve) => {
    server.listen(port, host, () =>
      resolve({
        messages,
        close: () => server.close(),
        async waitFor(pred, ms = 20000) {
          const end = Date.now() + ms;
          while (Date.now() < end) {
            const m = messages.find(pred);
            if (m) return m;
            await new Promise((r) => setTimeout(r, 200));
          }
          return null;
        },
      }),
    );
  });
}

// parse başlıkları ve gövdeyi ayırır; quoted-printable / base64 gövde çözülür
// (bağlantıları aramak için düz metin yeterli).
function parse(msg) {
  const [head, ...rest] = msg.data.split('\r\n\r\n');
  const headers = {};
  for (const l of head.replace(/\r\n[ \t]+/g, ' ').split('\r\n')) {
    const k = l.indexOf(':');
    if (k > 0) headers[l.slice(0, k).toLowerCase()] = l.slice(k + 1).trim();
  }
  let body = rest.join('\r\n\r\n');
  body = qp(body.replace(/=\r\n/g, ''));
  const b64 = body.match(/^[A-Za-z0-9+/=\r\n]{200,}$/m);
  if (b64) {
    try { body += '\n' + Buffer.from(b64[0].replace(/\s/g, ''), 'base64').toString('utf8'); } catch {}
  }
  let subject = headers.subject || '';
  subject = subject.replace(/=\?utf-8\?([bq])\?([^?]*)\?=/gi, (_, enc, s) =>
    enc.toLowerCase() === 'b' ? Buffer.from(s, 'base64').toString('utf8')
      : qp(s.replace(/_/g, ' ')));
  return { to: msg.to, headers, subject, body };
}

// qp quoted-printable kaçışlarını UTF-8 olarak çözer.
function qp(s) {
  return s.replace(/(?:=[0-9A-F]{2})+/gi, (m) => Buffer.from(m.replace(/=/g, ''), 'hex').toString('utf8'));
}
