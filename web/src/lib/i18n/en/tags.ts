import type { Shape } from '../types';
import type tr from '../tr/tags';

export default {
  title: 'Tags',
  intro:
    'Group monitors by environment, customer or team. When adding a tag to a monitor you can also give it an optional value (e.g. {example}). You can filter the monitor list by tag.',
  example: 'env: production',
  newTag: 'New tag',
  empty: 'No tags yet. Add your first tag here or create one from the monitor form.',
  monitorCount: '{count} monitor|{count} monitors',
  actionsFor: 'Actions for {name}',
  added: 'Tag “{name}” added',
  saved: 'Tag saved',
  deleteTitle: 'Delete tag',
  deleteMsg: 'Delete the tag “{name}”? It will be removed from {count} monitor.|Delete the tag “{name}”? It will be removed from {count} monitors.',
  deleted: 'Tag deleted',
  dialog: {
    edit: 'Edit tag',
    name: 'Tag name',
    namePlaceholder: 'e.g. environment, customer, team',
    color: 'Color',
    customColor: 'Pick a custom color',
    colorCode: 'Color code',
    preview: 'Preview',
    sampleName: 'tag',
    sampleValue: 'value',
    add: 'Add tag',
    errName: 'Tag name is required.',
    errLong: 'Tag name can be at most 50 characters.',
    errColor: 'Color must be in #rrggbb format (e.g. #2563eb).',
  },
  removeTag: 'Remove tag “{name}”',
} satisfies Shape<typeof tr>;
