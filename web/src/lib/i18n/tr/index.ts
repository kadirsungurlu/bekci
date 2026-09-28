// Ad alanlarının toplandığı yer. Yeni ad alanı eklemek dışında düzenlemeyin
// (her ad alanı kendi dosyasında; paralel çalışmada çakışma olmasın).

import common from './common';
import nav from './nav';
import status from './status';
import auth from './auth';
import account from './account';
import roles from './roles';
import settings from './settings';
import pub from './pub';
import pages from './pages';
import monitors from './monitors';
import monitorTypes from './monitorTypes';
import servers from './servers';
import alerts from './alerts';
import probes from './probes';
import notifications from './notifications';
import notifyTypes from './notifyTypes';
import incidents from './incidents';
import maintenance from './maintenance';
import users from './users';
import apiKeys from './apiKeys';
import backup from './backup';
import audit from './audit';
import tags from './tags';

const tr = {
  common,
  nav,
  status,
  auth,
  account,
  roles,
  settings,
  pub,
  pages,
  monitors,
  monitorTypes,
  servers,
  alerts,
  probes,
  notifications,
  notifyTypes,
  incidents,
  maintenance,
  users,
  apiKeys,
  backup,
  audit,
  tags,
};

export default tr;
