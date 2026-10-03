---
id: sign-in-sso
title: Giriş, şifre sıfırlama ve SSO (OpenID Connect)
nav: Giriş ve SSO
description: Kullanıcı e-postası ve "şifremi unuttum" akışı için sistem e-postası, Google / Microsoft / Keycloak / Authentik ile tek oturum açma (OIDC) kurulumu, rol eşlemesi ve iki adımlı doğrulamayla ilişkisi.
section: Kullanım
order: 14
slug: giris-ve-sso
---

Bekci'ye üç yolla girilir: **kullanıcı adı + şifre** (isteğe bağlı iki adımlı doğrulama), **şifre sıfırlama bağlantısı** (e-postayla) ve **tek oturum açma** (OpenID Connect ile şirket hesabı). Hepsi **Ayarlar → Giriş ve SSO** bölümünden yönetilir ve işlem kaydına yazılır.

## Kullanıcı e-postası {#e-posta}

Her hesabın isteğe bağlı bir e-postası olabilir: yönetici kullanıcı formundan girer, kullanıcı **Ayarlar → Hesabım → E-posta** ile kendisi değiştirir (mevcut şifre istenir; e-posta şifre sıfırlamanın anahtarıdır). Aynı e-posta iki hesapta olamaz. E-posta küçük harfle saklanır; giriş ekranında kullanıcı adı yerine e-posta da kabul edilir (yalnızca "şifremi unuttum" için).

## Şifremi unuttum {#sifremi-unuttum}

1. Önce bir **e-posta (SMTP) bildirim kanalı** oluşturun (Bildirimler → Yeni kanal → E-posta) ve **Test gönder** ile çalıştığını doğrulayın.
2. **Ayarlar → Giriş ve SSO → Sistem e-postası** altında bu kanalı seçin. Bekci sıfırlama e-postalarını bu kanalın SMTP ayarıyla gönderir; alıcı kullanıcının kendi adresidir (kanaldaki "alıcılar" kullanılmaz).
3. Giriş ekranında **Şifremi unuttum** bağlantısı görünür. Kullanıcı adını ya da e-postasını yazan kullanıcıya, hesabında e-posta kayıtlıysa bağlantı gider.

Güvenlik kuralları:

- Yanıt her durumda aynıdır ("hesap kayıtlı bir e-postaya bağlıysa bağlantı gönderildi"); hesabın var olup olmadığı sızmaz.
- IP başına 15 dakikada 10, hesap başına 3 istek; sıfırlama denemeleri de IP başına sınırlıdır.
- Bağlantı **30 dakika** geçerli ve **tek kullanımlık**tır; veritabanında token'ın yalnızca SHA-256 özeti saklanır. Yeni istek eskisini geçersiz kılar.
- Şifre değişince kullanıcının **tüm oturumları kapanır**; iki adımlı doğrulama açıksa açık kalır (telefon da kaybolduysa yönetici **2FA'yı sıfırla** der ya da [komut satırı](/docs/sorun-giderme/) kullanılır).
- Bağlantıdaki adres `BASE_URL` ortam değişkeninden üretilir; ayarlı değilse isteğin sunucu adı kullanılır. Ters vekil arkasında `BASE_URL` verin.
- Devre dışı hesaplara ve SSO ile açılmış şifresiz hesaplara bağlantı gitmez.

## Tek oturum açma (OpenID Connect) {#sso}

Bekci her **OpenID Connect** sağlayıcısıyla çalışır: Google Workspace, Microsoft Entra ID (Azure AD), Keycloak, Authentik, Authelia, Okta, Zitadel… Akış: *authorization code* + **PKCE (S256)**, **state** ve **nonce** denetimi, `id_token` imzasının sağlayıcının JWKS anahtarlarıyla doğrulanması. Gizli anahtar (client secret) veritabanında saklanır, API yanıtlarında ve yedeklerde maskelidir.

### Kurulum adımları {#sso-kurulum}

1. **Ayarlar → Giriş ve SSO → Tek oturum açma** bölümündeki **yönlendirme adresini** kopyalayın: `https://⟦bekci.ornek.com⟧/api/auth/oidc/callback`. Bu adresin doğru olması için `BASE_URL` ortam değişkeni panelin dış adresi olmalı; ters vekil `X-Forwarded-Proto: https` göndermeli.
2. Sağlayıcınızda bir **web uygulaması** istemcisi (client) oluşturun, yönlendirme adresini kaydedin, **client ID** ve **client secret** alın.
3. Bekci'de **Issuer adresi**, client ID ve secret'ı girin; **Keşfi dene** ile `/.well-known/openid-configuration` okunduğunu görün. Kapsamlar varsayılan `openid profile email`.
4. Hesap eşlemesini seçin ve **SSO girişi açık** kutusunu işaretleyin. Kaydederken Bekci issuer'ın keşif belgesini okur (en fazla 10 sn): adrese ulaşılamıyorsa, adres bir OpenID Connect sağlayıcısı değilse ya da sağlayıcının bildirdiği issuer girilenle uyuşmuyorsa ayar kaydedilmez ve nedeni yazılır (SSO kapalıyken ya da issuer değişmeden yapılan düzenlemelerde keşif yapılmaz). Giriş ekranında artık **"… ile giriş"** düğmesi var. İsterseniz şifre formunu gizleyin (bir bağlantıyla yine açılır; yöneticiler kilitli kalmaz).

### Hesap eşlemesi ve roller {#sso-hesap}

Girişte hesap şu sırayla bulunur:

1. **Daha önce bağlanmış kimlik** (issuer + subject) → doğrudan o hesap.
2. **E-postası eşleşen yerel hesap** (*E-postası eşleşen yerel hesaba bağla* açıksa ve sağlayıcı e-postayı doğrulanmış veriyorsa) → hesaba bağlanır; bundan sonra subject ile tanınır.
3. **Hesap aç** (*Eşleşmeyen kullanıcılar için hesap aç* açıksa): kullanıcı adı `preferred_username` (ya da seçtiğiniz claim, yoksa e-postanın @ öncesi) sadeleştirilerek üretilir, çakışırsa `-2`, `-3` eklenir. Hesabın şifresi yoktur (rastgele, kullanılamaz özet); kullanıcı yalnızca SSO ile girer. Kapalıysa bilinmeyen kullanıcı "bu kimliğe bağlı hesap yok" görür.

**Rol eşlemesi** (isteğe bağlı): bir claim adı (ör. `groups`, `roles`) ve yönetici / editör / izleyici yapan değerleri girin. Değerler virgülle ayrılır, büyük/küçük harf duyarsızdır; birden çok eşleşmede en yüksek rol kazanır. Rol **her girişte** yeniden hesaplanır — grubundan çıkarılan kullanıcı bir sonraki girişte düşer; son aktif yönetici hiçbir eşlemeyle düşürülmez. Eşleşme yoksa mevcut hesabın rolü değişmez, yeni hesap **varsayılan rolü** alır. Rol claim'i boşsa roller yalnızca panelden yönetilir.

Kısıtlı (müşteri) görünürlüğü SSO ile değişmez: hesap daha önce kısıtlıysa kısıtlı kalır; yeni açılan hesaplar tüm monitörleri görür (rolleri gereği).

### İki adımlı doğrulama ve SSO {#sso-2fa}

SSO ile giren kullanıcıya Bekci'nin **yerel TOTP kodu sorulmaz**; ikinci faktör (telefon doğrulaması, güvenlik anahtarı, koşullu erişim) sağlayıcının işidir ve orada zorunlu kılınmalıdır. Aynı hesap **şifreyle** girerse TOTP yine istenir. Yöneticinin verdiği geçici şifreyle SSO üzerinden ilk kez giren kullanıcıya "şifre değiştir" ekranı gösterilmez; geçici şifre anlamını yitirir. API anahtarları ve şifre değişimi gibi hesap güvenliği uçları SSO için de aynıdır.

### Sağlayıcı örnekleri {#sso-ornekler}

:::tabs key=idp label="Sağlayıcı"
@tab Google
- Google Cloud Console → **APIs & Services → Credentials → Create credentials → OAuth client ID**, tür **Web application**.
- Authorized redirect URI: `https://⟦bekci.ornek.com⟧/api/auth/oidc/callback`.
- Bekci: Issuer `https://accounts.google.com`, kapsamlar `openid profile email`.
- Google grup bilgisi vermez; rol eşlemesini boş bırakıp rolleri panelden yönetin ya da yalnızca Workspace alanınızın girebilmesi için *Hesap aç* kutusunu kapatıp hesapları önceden açın (e-postayla bağlanır).
@tab Microsoft
- Entra admin center → **App registrations → New registration**, Redirect URI (Web): `https://⟦bekci.ornek.com⟧/api/auth/oidc/callback`; **Certificates & secrets** altında bir client secret oluşturun.
- Issuer: `https://login.microsoftonline.com/⟦TENANT-ID⟧/v2.0` (tek kiracı). `email` claim'i için **Token configuration → Add optional claim → ID → email**.
- Roller için **App roles** tanımlayıp `roles` claim'ini kullanın (Rol claim'i: `roles`; değerler app role'un *value* alanı). Grupları kullanacaksanız **Token configuration → Add groups claim** (`groups`, grup ID'leri döner; değer alanına ID yazın).
@tab Keycloak
- Realm → **Clients → Create client**: Client type OpenID Connect, *Client authentication* açık, Valid redirect URI `https://⟦bekci.ornek.com⟧/api/auth/oidc/callback`.
- Issuer: `https://⟦keycloak.ornek.com⟧/realms/⟦realm⟧`.
- Gruplar için client'a bir **Group Membership** mapper ekleyin (Token Claim Name `groups`, *Full group path* kapalı) ve Bekci'de Rol claim'i `groups`, değerler grup adları (ör. `bekci-admins`). `preferred_username` ve `email` standart kapsamlarla gelir; e-postanın doğrulanmış olması için kullanıcıda *Email verified* açık olmalı.
@tab Authentik
- **Applications → Providers → Create → OAuth2/OpenID Provider**: Client type Confidential, Redirect URI `https://⟦bekci.ornek.com⟧/api/auth/oidc/callback`, Signing key seçili; ardından bir **Application** oluşturup bu provider'a bağlayın.
- Issuer: provider'ın **OpenID Configuration Issuer** değeri (`https://⟦auth.ornek.com⟧/application/o/⟦slug⟧/`); Bekci sondaki `/` işaretini kaldırır.
- Gruplar `openid profile email` kapsamlarıyla `groups` claim'inde gelir: Rol claim'i `groups`, değerler Authentik grup adları.
:::

### Sorun giderme {#sso-sorun}

| Giriş ekranındaki hata | Neden / çözüm |
|---|---|
| *Giriş oturumu doğrulanamadı* | 10 dakikalık state süresi doldu, çerez engellendi ya da sayfa farklı bir alan adından açıldı. Yönlendirme adresi ile panel adresi aynı olmalı (`BASE_URL`). |
| *Sağlayıcıdan erişim alınamadı* | Client secret yanlış ya da sağlayıcıda kayıtlı yönlendirme adresi farklı. Keşfi dene düğmesi çalışıyorsa sorun secret/redirect'tedir. |
| *Kimlik belirteci doğrulanamadı* | Issuer adresi token'daki `iss` ile birebir aynı değil (sondaki `/`, `v2.0`, realm adı). |
| *Bu kimliğe bağlı hesap yok* | *Hesap aç* kapalı ve e-posta eşleşmedi (ya da e-posta doğrulanmamış). Hesabı önceden açıp e-postasını girin ya da hesap açmayı etkinleştirin. |
| *Rol veren grup yok* | Rol claim'i ayarlı, varsayılan rol boş ve kullanıcı hiçbir değere uymuyor. |

Sunucu günlüğünde (`LOG_LEVEL=info`) her başarısız SSO girişi `OIDC girişi başarısız` satırıyla, nedeniyle birlikte yer alır; işlem kaydında başarılı girişler **SSO ile giriş yaptı**, açılan hesaplar **SSO ile hesap açıldı** olarak görünür.
