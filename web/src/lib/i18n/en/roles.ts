import type { Shape } from '../types';
import type tr from '../tr/roles';

export default {
  admin: 'Admin',
  editor: 'Editor',
  viewer: 'Viewer',
  adminDesc: 'Manages everything, including users, settings and the audit log.',
  editorDesc: 'Manages monitors, notifications, status pages and maintenance.',
  viewerDesc: "View only; can't change anything.",
} satisfies Shape<typeof tr>;
