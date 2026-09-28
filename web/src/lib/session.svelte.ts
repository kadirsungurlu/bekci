// Giriş yapmış kullanıcı ve rolüne göre arayüz yetkileri.
// Asıl yetki kontrolü sunucudadır; burada yalnızca yapılamayacak işlemlerin
// düğmeleri ve menüleri gizlenir.

import type { Role, User } from './api';
import { t } from './i18n';

/** Rol adları ve açıklamaları (getter: her okumada geçerli dilde → reaktif). */
export const ROLE_LABELS: Record<Role, string> = {
  get admin() {
    return t('roles.admin');
  },
  get editor() {
    return t('roles.editor');
  },
  get viewer() {
    return t('roles.viewer');
  },
};

export const ROLE_DESCS: Record<Role, string> = {
  get admin() {
    return t('roles.adminDesc');
  },
  get editor() {
    return t('roles.editorDesc');
  },
  get viewer() {
    return t('roles.viewerDesc');
  },
};

const RANK: Record<Role, number> = { viewer: 1, editor: 2, admin: 3 };

export const roleRank = (r: Role) => RANK[r] ?? 0;

class Session {
  user = $state<User | null>(null);
  version = $state('');

  role = $derived<Role>(this.user?.role ?? 'viewer');
  /** Monitör, bildirim, durum sayfası ve bakım ekleyip değiştirebilir. */
  canEdit = $derived(roleRank(this.role) >= RANK.editor);
  isAdmin = $derived(this.role === 'admin');
  /** Yalnızca seçili monitörleri gören (müşteri) izleyici: sunucu takibini göremez. */
  restricted = $derived(!!this.user && !this.user.all_monitors);
  /** Sunucular ekranı: kısıtsız kullanıcıda her zaman, müşteride kendisine sunucu atanmışsa. */
  canSeeServers = $derived(!!this.user && (this.user.servers ?? !this.restricted));
  displayName = $derived(this.user ? this.user.display_name || this.user.username : '');

  set(u: User | null) {
    this.user = u;
  }
}

export const session = new Session();
