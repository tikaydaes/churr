"""Colour themes (ruling R61): an account's own themes, the server's theme library, and the server's defaults."""
from lib import area, op

area('Themes',
     "Colour themes: each account's own, the server's library of published ones, and the server's default dark and "
     'light themes. A theme stores its base colours only; clients derive the rest by the rules of docs/STYLE.md, '
     'and the server checks the contrast of the result with the same arithmetic.')

op('GET me/themes', 'listMyThemes',
   "List the caller's own themes",
   who='Members, admins, the owner and guests. Not bots.',
   desc='The whole list, oldest first; at most 50.',
   res={'themes': '[#Theme]'},
   err='forbidden')

op('POST me/themes', 'createTheme',
   'Save a new theme',
   who='Members, admins, the owner and guests. Not bots.',
   desc='Not published. An account has at most 50 themes (`conflict`).',
   body='#ThemeInput',
   res={'theme': '#Theme'},
   st=201,
   err='forbidden conflict')

op('GET themes/{id}', 'getTheme',
   'Look at one theme',
   who="Any signed-in account. The id is the theme's link: whoever has it may look, published or not.",
   res={'theme': '#Theme'},
   err='not_found')

op('PATCH me/themes/{id}', 'updateTheme',
   "Change one of the caller's themes",
   who='Its owner. Anyone else gets `not_found`.',
   desc='A published theme stays published, and `theme.published` tells everyone of the change.',
   body='#ThemeInput',
   res={'theme': '#Theme'},
   err='forbidden not_found')

op('DELETE me/themes/{id}', 'deleteTheme',
   "Delete one of the caller's themes",
   who='Its owner. Anyone else gets `not_found`.',
   desc='A published theme leaves the library (`theme.unpublished`). Copies others added, theme cards in messages '
        'and a server default made from it are copies and stay.',
   err='forbidden not_found')

op('POST me/themes/{id}/publish', 'publishTheme',
   "Put one of the caller's themes in the server's theme library",
   who='Its owner, if a member, admin or the owner. Not guests.',
   desc='Sends `theme.published`. Publishing a published theme changes nothing.',
   res={'theme': '#Theme'},
   err='forbidden not_found')

op('DELETE me/themes/{id}/publish', 'unpublishTheme',
   "Take one of the caller's themes out of the library",
   who='Its owner, if a member, admin or the owner. Not guests.',
   desc='Sends `theme.unpublished`. Copies others added stay theirs.',
   res={'theme': '#Theme'},
   err='forbidden not_found')

op('GET themes', 'listThemeLibrary',
   "List the server's theme library",
   who='Members, admins, the owner and guests. Not bots.',
   desc='Published themes, most recently published first.',
   q=['q:str:Only themes whose name contains this, ignoring case. At most 64 characters.'],
   res={'themes': '[#Theme]'},
   err='forbidden',
   page='cursor')

op('POST themes/{id}/add', 'addTheme',
   'Add a copy of a theme to the caller\'s own',
   who='Members, admins, the owner and guests. Not bots.',
   desc='A theme from the library, or any theme by its link. The copy is the caller\'s, under the same name, not '
        'published; it counts towards their 50.',
   res={'theme': '#Theme'},
   st=201,
   err='forbidden not_found conflict')

op('DELETE admin/themes/{id}', 'removeLibraryTheme',
   'Remove a theme from the library',
   who='Admins (the owner included).',
   desc='The theme is unpublished; its owner keeps it. Sends `theme.unpublished`, and is in the audit log '
        '(`theme.removed`). `not_found` for a theme that is not in the library.',
   err='forbidden not_found')

op('PUT admin/theme-defaults', 'setDefaultTheme',
   "Set or clear the server's default dark or light theme",
   who='Admins (the owner included).',
   desc="With `theme_id`, the default of that kind becomes a copy of that theme, which must be of the kind "
        "(`invalid_value` otherwise); without, it is cleared. The defaults are in the server's description "
        "(`default_themes`), so `server.updated` tells everyone. In the audit log as `theme.default_set` or "
        '`theme.default_cleared`.',
   body={'kind': 'enum:dark|light', 'theme_id?': 'uuid'},
   res={'default_themes': '#DefaultThemes'},
   err='forbidden not_found')
