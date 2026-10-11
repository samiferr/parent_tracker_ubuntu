/* @ds-bundle: {"format":4,"namespace":"Lumen","components":[{"name":"Button"},{"name":"ActionButton"},{"name":"TextField"},{"name":"SearchField"},{"name":"TextArea"},{"name":"Checkbox"},{"name":"Switch"},{"name":"RadioGroup"},{"name":"Tabs"},{"name":"StatusLight"},{"name":"Badge"},{"name":"ProgressBar"},{"name":"InlineAlert"},{"name":"Divider"},{"name":"Card"},{"name":"Table"},{"name":"List"},{"name":"Disclosure"},{"name":"Accordion"},{"name":"Tree"},{"name":"Pagination"},{"name":"Avatar"},{"name":"AvatarGroup"},{"name":"Dropdown"},{"name":"Menu"},{"name":"Steps"},{"name":"Timeline"},{"name":"Drawer"},{"name":"EmptyState"},{"name":"LoginForm"},{"name":"Icon"}]} */
(function () {
  var React = window.React;
  var h = React.createElement;
  var useState = React.useState, useId = React.useId;
  function cx() { return Array.prototype.filter.call(arguments, Boolean).join(' '); }

  /* Status glyphs: filled shapes with a Lucide glyph cut out (Lucide ships no filled set) */
  var STATUS = {
    info: { base: ['circle', { cx: 12, cy: 12, r: 10 }], glyph: [['path', { d: 'M12 16v-4' }], ['path', { d: 'M12 8h.01' }]] },
    positive: { base: ['circle', { cx: 12, cy: 12, r: 10 }], glyph: [['path', { d: 'm9 12 2 2 4-4' }]] },
    negative: { base: ['circle', { cx: 12, cy: 12, r: 10 }], glyph: [['path', { d: 'M12 8v4' }], ['path', { d: 'M12 16h.01' }]] },
    notice: { base: ['path', { d: 'm21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z' }], glyph: [['path', { d: 'M12 9v4' }], ['path', { d: 'M12 17h.01' }]] }
  };
  function StatusGlyph(props) {
    var id = 'lmsg' + String(useId()).replace(/[^a-zA-Z0-9]/g, ''), def = STATUS[props.name];
    var baseAttrs = Object.assign({ fill: 'currentColor', mask: 'url(#' + id + ')' }, def.base[1]);
    return h('svg', { width: props.size || 16, height: props.size || 16, viewBox: '0 0 24 24', 'aria-hidden': 'true', focusable: 'false', className: props.className },
      h('mask', { id: id }, h('rect', { width: 24, height: 24, fill: '#fff' }),
        h('g', { fill: 'none', stroke: '#000', strokeWidth: 2.2, strokeLinecap: 'round', strokeLinejoin: 'round' }, def.glyph.map(function (n, k) { return h(n[0], Object.assign({ key: k }, n[1])); }))),
      h(def.base[0], baseAttrs));
  }

  /* Icon: Lucide (ISC licence) outline icons, 24 grid, drawn at Spectrum sizes with a 2.4 stroke (2px at 20) */
  var ICON_SET = {
    add: [["path",{"d":"M5 12h14"}],["path",{"d":"M12 5v14"}]],
    alert: [["path",{"d":"m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"}],["path",{"d":"M12 9v4"}],["path",{"d":"M12 17h.01"}]],
    'arrow-down': [["path",{"d":"M12 5v14"}],["path",{"d":"m19 12-7 7-7-7"}]],
    'arrow-left': [["path",{"d":"m12 19-7-7 7-7"}],["path",{"d":"M19 12H5"}]],
    'arrow-right': [["path",{"d":"M5 12h14"}],["path",{"d":"m12 5 7 7-7 7"}]],
    'arrow-up': [["path",{"d":"m5 12 7-7 7 7"}],["path",{"d":"M12 19V5"}]],
    bell: [["path",{"d":"M10.268 21a2 2 0 0 0 3.464 0"}],["path",{"d":"M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"}]],
    bookmark: [["path",{"d":"M17 3a2 2 0 0 1 2 2v15a1 1 0 0 1-1.496.868l-4.512-2.578a2 2 0 0 0-1.984 0l-4.512 2.578A1 1 0 0 1 5 20V5a2 2 0 0 1 2-2z"}]],
    calendar: [["path",{"d":"M8 2v3"}],["path",{"d":"M16 2v3"}],["rect",{"x":"3","y":"3","width":"18","height":"18","rx":"2"}],["path",{"d":"M3 9h18"}]],
    chat: [["path",{"d":"M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z"}]],
    check: [["path",{"d":"M20 6 9 17l-5-5"}]],
    'chevron-down': [["path",{"d":"m6 9 6 6 6-6"}]],
    'chevron-left': [["path",{"d":"m15 18-6-6 6-6"}]],
    'chevron-right': [["path",{"d":"m9 18 6-6-6-6"}]],
    'chevron-up': [["path",{"d":"m18 15-6-6-6 6"}]],
    clock: [["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"M12 6v6l4 2"}]],
    close: [["path",{"d":"M18 6 6 18"}],["path",{"d":"m6 6 12 12"}]],
    copy: [["rect",{"width":"14","height":"14","x":"8","y":"8","rx":"2","ry":"2"}],["path",{"d":"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"}]],
    delete: [["path",{"d":"M10 11v6"}],["path",{"d":"M14 11v6"}],["path",{"d":"M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"}],["path",{"d":"M3 6h18"}],["path",{"d":"M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"}]],
    download: [["path",{"d":"M12 15V3"}],["path",{"d":"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"}],["path",{"d":"m7 10 5 5 5-5"}]],
    edit: [["path",{"d":"M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"}],["path",{"d":"m15 5 4 4"}]],
    error: [["circle",{"cx":"12","cy":"12","r":"10"}],["line",{"x1":"12","x2":"12","y1":"8","y2":"12"}],["line",{"x1":"12","x2":"12.01","y1":"16","y2":"16"}]],
    'external-link': [["path",{"d":"M15 3h6v6"}],["path",{"d":"M10 14 21 3"}],["path",{"d":"M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"}]],
    eye: [["path",{"d":"M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"}],["circle",{"cx":"12","cy":"12","r":"3"}]],
    'eye-off': [["path",{"d":"M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"}],["path",{"d":"M14.084 14.158a3 3 0 0 1-4.242-4.242"}],["path",{"d":"M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"}],["path",{"d":"m2 2 20 20"}]],
    file: [["path",{"d":"M6 22a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.704.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2z"}],["path",{"d":"M14 2v5a1 1 0 0 0 1 1h5"}]],
    filter: [["path",{"d":"M10 20a1 1 0 0 0 .553.895l2 1A1 1 0 0 0 14 21v-7a2 2 0 0 1 .517-1.341L21.74 4.67A1 1 0 0 0 21 3H3a1 1 0 0 0-.742 1.67l7.225 7.989A2 2 0 0 1 10 14z"}]],
    folder: [["path",{"d":"M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"}]],
    globe: [["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"}],["path",{"d":"M2 12h20"}]],
    grid: [["rect",{"width":"7","height":"7","x":"3","y":"3","rx":"1"}],["rect",{"width":"7","height":"7","x":"14","y":"3","rx":"1"}],["rect",{"width":"7","height":"7","x":"14","y":"14","rx":"1"}],["rect",{"width":"7","height":"7","x":"3","y":"14","rx":"1"}]],
    heart: [["path",{"d":"M2 9.5a5.5 5.5 0 0 1 9.591-3.676.56.56 0 0 0 .818 0A5.49 5.49 0 0 1 22 9.5c0 2.29-1.5 4-3 5.5l-5.492 5.313a2 2 0 0 1-3 .019L5 15c-1.5-1.5-3-3.2-3-5.5"}]],
    help: [["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"}],["path",{"d":"M12 17h.01"}]],
    home: [["path",{"d":"M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"}],["path",{"d":"M3 10a2 2 0 0 1 .709-1.528l7-6a2 2 0 0 1 2.582 0l7 6A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"}]],
    image: [["rect",{"width":"18","height":"18","x":"3","y":"3","rx":"2","ry":"2"}],["circle",{"cx":"9","cy":"9","r":"2"}],["path",{"d":"m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"}]],
    info: [["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"M12 16v-4"}],["path",{"d":"M12 8h.01"}]],
    link: [["path",{"d":"M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"}],["path",{"d":"M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"}]],
    list: [["path",{"d":"M3 5h.01"}],["path",{"d":"M3 12h.01"}],["path",{"d":"M3 19h.01"}],["path",{"d":"M8 5h13"}],["path",{"d":"M8 12h13"}],["path",{"d":"M8 19h13"}]],
    lock: [["rect",{"width":"18","height":"11","x":"3","y":"11","rx":"2","ry":"2"}],["path",{"d":"M7 11V7a5 5 0 0 1 10 0v4"}]],
    'log-out': [["path",{"d":"m16 17 5-5-5-5"}],["path",{"d":"M21 12H9"}],["path",{"d":"M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"}]],
    mail: [["path",{"d":"m22 7-8.991 5.727a2 2 0 0 1-2.009 0L2 7"}],["rect",{"x":"2","y":"4","width":"20","height":"16","rx":"2"}]],
    menu: [["path",{"d":"M4 5h16"}],["path",{"d":"M4 12h16"}],["path",{"d":"M4 19h16"}]],
    more: [["circle",{"cx":"12","cy":"12","r":"1"}],["circle",{"cx":"19","cy":"12","r":"1"}],["circle",{"cx":"5","cy":"12","r":"1"}]],
    pause: [["rect",{"x":"14","y":"3","width":"5","height":"18","rx":"1"}],["rect",{"x":"5","y":"3","width":"5","height":"18","rx":"1"}]],
    play: [["path",{"d":"M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z"}]],
    refresh: [["path",{"d":"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"}],["path",{"d":"M21 3v5h-5"}],["path",{"d":"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"}],["path",{"d":"M8 16H3v5"}]],
    remove: [["path",{"d":"M5 12h14"}]],
    search: [["path",{"d":"m21 21-4.34-4.34"}],["circle",{"cx":"11","cy":"11","r":"8"}]],
    share: [["path",{"d":"M12 2v13"}],["path",{"d":"m16 6-4-4-4 4"}],["path",{"d":"M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"}]],
    sliders: [["path",{"d":"M10 5H3"}],["path",{"d":"M12 19H3"}],["path",{"d":"M14 3v4"}],["path",{"d":"M16 17v4"}],["path",{"d":"M21 12h-9"}],["path",{"d":"M21 19h-5"}],["path",{"d":"M21 5h-7"}],["path",{"d":"M8 10v4"}],["path",{"d":"M8 12H3"}]],
    sort: [["path",{"d":"m21 16-4 4-4-4"}],["path",{"d":"M17 20V4"}],["path",{"d":"m3 8 4-4 4 4"}],["path",{"d":"M7 4v16"}]],
    star: [["path",{"d":"M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z"}]],
    success: [["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"m16 9-5.5 5.5L8 12"}]],
    unlock: [["rect",{"width":"18","height":"11","x":"3","y":"11","rx":"2","ry":"2"}],["path",{"d":"M7 11V7a5 5 0 0 1 9.9-1"}]],
    upload: [["path",{"d":"M12 3v12"}],["path",{"d":"m17 8-5-5-5 5"}],["path",{"d":"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"}]],
    user: [["path",{"d":"M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"}],["circle",{"cx":"12","cy":"7","r":"4"}]],
    users: [["path",{"d":"M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"}],["path",{"d":"M16 3.128a4 4 0 0 1 0 7.744"}],["path",{"d":"M22 21v-2a4 4 0 0 0-3-3.87"}],["circle",{"cx":"9","cy":"7","r":"4"}]]
  };
  var ICON_SIZES = { xs: 14, s: 16, m: 20, l: 22, xl: 26 };
  var ICON_NAMES = Object.keys(ICON_SET).sort();
  function Icon(props) {
    var size = typeof props.size === 'number' ? props.size : (ICON_SIZES[props.size || 'm'] || 20);
    var nodes = ICON_SET[props.name];
    if (!nodes) return null;
    var label = props['aria-label'] || props.label;
    return h('svg', { width: size, height: size, viewBox: '0 0 24 24', className: cx('lm-icon', props.className), role: label ? 'img' : undefined, 'aria-label': label, 'aria-hidden': label ? undefined : 'true', focusable: 'false',
      fill: 'none', stroke: 'currentColor', strokeWidth: props.strokeWidth || 2.4, strokeLinecap: 'round', strokeLinejoin: 'round' },
      nodes.map(function (n, i) { return h(n[0], Object.assign({ key: i }, n[1])); }));
  }
  function iconNode(icon, size) { return typeof icon === 'string' ? h(Icon, { name: icon, size: size }) : icon; }
  var BTN_ICON = { s: 's', m: 'm', l: 'l', xl: 'xl' };

  /* Button */
  function Button(props) {
    var variant = props.variant || 'accent', size = props.size || 'm';
    return h('button', {
      type: props.type || 'button',
      className: cx('lm-root lm-focusable lm-button', 'lm-button--' + variant, 'lm-button--' + (props.outline ? 'outline' : 'fill'), size !== 'm' && 'lm-button--' + size, props.className),
      'aria-label': props['aria-label'],
      disabled: props.isDisabled,
      onClick: props.onPress
    }, iconNode(props.icon, ICON_SIZES[BTN_ICON[size]]), props.children);
  }

  /* ActionButton */
  function ActionButton(props) {
    var size = props.size || 'm', hasText = props.children != null && props.children !== false;
    return h('button', {
      type: 'button',
      className: cx('lm-root lm-focusable lm-action', props.isQuiet && 'lm-action--quiet', size !== 'm' && 'lm-action--' + size, !hasText && 'lm-action--icononly', hasText && props.icon && 'lm-action--hasicon', props.className),
      'aria-label': props['aria-label'],
      'aria-pressed': props.isSelected === undefined ? undefined : !!props.isSelected,
      disabled: props.isDisabled,
      onClick: props.onPress
    }, iconNode(props.icon, ICON_SIZES[size]), props.children);
  }

  /* TextField */
  function TextField(props) {
    var id = useId(), helpId = id + '-help';
    var rv = useState(false), shown = rv[0], setShown = rv[1];
    var invalid = props.validationState === 'invalid';
    var help = invalid ? props.errorMessage : props.description;
    var type = props.revealable && shown ? 'text' : (props.type || 'text');
    var input = h('input', {
      id: id, ref: props.inputRef, className: cx('lm-input', props.revealable && 'lm-input--revealable', invalid && !props.revealable && 'lm-input--alert'), type: type, name: props.name, autoComplete: props.autoComplete, placeholder: props.placeholder,
      defaultValue: props.defaultValue, value: props.value, disabled: props.isDisabled, required: props.isRequired,
      'aria-invalid': invalid ? 'true' : undefined, 'aria-describedby': help ? helpId : undefined,
      onChange: props.onChange ? function (e) { props.onChange(e.target.value); } : undefined
    });
    return h('div', { className: 'lm-root lm-field', style: props.width ? { width: props.width } : null },
      props.label ? h('label', { className: 'lm-label', htmlFor: id }, props.label,
        props.isRequired ? h('span', { className: 'lm-label__req', 'aria-hidden': 'true' }, '*') : null) : null,
      h('div', { className: 'lm-input-wrap' }, input,
        props.revealable ? h('button', { type: 'button', className: 'lm-focusable lm-input-toggle', 'aria-controls': id, disabled: props.isDisabled, onClick: function () { setShown(!shown); } }, shown ? 'Hide' : 'Show')
          : (invalid ? h(StatusGlyph, { name: 'negative', size: 16, className: 'lm-input-alert' }) : null)),
      help ? h('div', { id: helpId, className: cx('lm-help', invalid && 'lm-help--error') },
        invalid ? h(StatusGlyph, { name: 'negative', size: 14 }) : null, h('span', null, help)) : null);
  }

  /* TextArea: multi-line text input */
  function TextArea(props) {
    var id = useId(), helpId = id + '-help';
    var invalid = props.validationState === 'invalid';
    var help = invalid ? props.errorMessage : props.description;
    return h('div', { className: cx('lm-root lm-field', props.className), style: props.width ? { width: props.width } : null },
      props.label ? h('label', { className: 'lm-label', htmlFor: id }, props.label,
        props.isRequired ? h('span', { className: 'lm-label__req', 'aria-hidden': 'true' }, '*') : null) : null,
      h('textarea', {
        id: id, className: cx('lm-input', 'lm-textarea', props.resize === 'none' && 'lm-textarea--fixed'), rows: props.rows || 3, name: props.name, placeholder: props.placeholder,
        defaultValue: props.defaultValue, value: props.value, disabled: props.isDisabled, required: props.isRequired, maxLength: props.maxLength,
        'aria-invalid': invalid ? 'true' : undefined, 'aria-describedby': help ? helpId : undefined,
        onChange: props.onChange ? function (e) { props.onChange(e.target.value); } : undefined
      }),
      help ? h('div', { id: helpId, className: cx('lm-help', invalid && 'lm-help--error') },
        invalid ? h(StatusGlyph, { name: 'negative', size: 14 }) : null, h('span', null, help)) : null);
  }

  /* SearchField: search input with icon, clear control and optional submit button */
  var SF_SIZE = { s: { cls: 'lm-search--s', icon: 14, btn: 's' }, m: { cls: '', icon: 16, btn: 'm' }, l: { cls: 'lm-search--l', icon: 18, btn: 'l' } };
  function SearchField(props) {
    var id = useId(), helpId = id + '-help';
    var size = SF_SIZE[props.size || 'm'];
    var st = useState(props.defaultValue || ''), inner = st[0], setInner = st[1];
    var controlled = props.value !== undefined, value = controlled ? props.value : inner;
    var ref = React.useRef(null);
    function set(v) { if (!controlled) setInner(v); if (props.onChange) props.onChange(v); }
    function clear() { set(''); if (props.onClear) props.onClear(); if (ref.current) ref.current.focus(); }
    function submit(e) { e.preventDefault(); if (props.onSubmit) props.onSubmit(value); }
    var label = props.label, hasBtn = !!props.buttonLabel || !!props.buttonIcon;
    var input = h('div', { className: 'lm-search__box' },
      h('span', { className: 'lm-search__icon', 'aria-hidden': 'true' }, h(Icon, { name: 'search', size: size.icon })),
      h('input', {
        id: id, ref: ref, type: 'search', className: 'lm-input lm-search__input', value: value, name: props.name, placeholder: props.placeholder || 'Search',
        autoComplete: 'off', disabled: props.isDisabled, enterKeyHint: 'search', 'aria-label': label ? undefined : (props['aria-label'] || 'Search'),
        'aria-describedby': props.description ? helpId : undefined,
        onChange: function (e) { set(e.target.value); },
        onKeyDown: function (e) { if (e.key === 'Escape' && value) { e.preventDefault(); clear(); } }
      }),
      value && !props.isDisabled ? h('button', { type: 'button', className: 'lm-focusable lm-search__clear', 'aria-label': 'Clear search', onClick: clear },
        h(Icon, { name: 'close', size: 14 })) : null);
    return h('form', { role: 'search', className: cx('lm-root lm-field lm-search', size.cls, hasBtn && 'lm-search--withbtn', props.className), style: props.width ? { width: props.width } : null, onSubmit: submit, noValidate: true },
      label ? h('label', { className: 'lm-label', htmlFor: id }, label) : null,
      h('div', { className: 'lm-search__row' }, input,
        hasBtn ? h(Button, { type: 'submit', variant: props.buttonVariant || 'accent', size: size.btn, icon: props.buttonIcon, className: props.buttonLabel ? null : 'lm-search__btn--icon', 'aria-label': props.buttonLabel ? undefined : 'Search', isDisabled: props.isDisabled }, props.buttonLabel) : null),
      props.description ? h('div', { id: helpId, className: 'lm-help' }, h('span', null, props.description)) : null);
  }

  /* Checkbox */
  function Checkbox(props) {
    return h('label', { className: cx('lm-root lm-check', props.isDisabled && 'lm-check--disabled', props.className) },
      h('input', {
        type: 'checkbox', 'aria-label': props['aria-label'], defaultChecked: props.defaultSelected, checked: props.isSelected, disabled: props.isDisabled,
        ref: function (el) { if (el) el.indeterminate = !!props.isIndeterminate; },
        onChange: props.onChange ? function (e) { props.onChange(e.target.checked); } : undefined
      }),
      h('span', { className: 'lm-check__box', 'aria-hidden': 'true' },
        h(Icon, { name: props.isIndeterminate ? 'remove' : 'check', size: 12, strokeWidth: 3.6 })),
      props.children);
  }

  /* Switch */
  function Switch(props) {
    return h('label', { className: cx('lm-root lm-switch', props.isDisabled && 'lm-switch--disabled', props.className) },
      h('input', {
        type: 'checkbox', role: 'switch', defaultChecked: props.defaultSelected, checked: props.isSelected, disabled: props.isDisabled,
        onChange: props.onChange ? function (e) { props.onChange(e.target.checked); } : undefined
      }),
      h('span', { className: 'lm-switch__track', 'aria-hidden': 'true' }),
      props.children);
  }

  /* RadioGroup */
  function RadioGroup(props) {
    var name = useId();
    var st = useState(props.defaultValue), val = st[0], setVal = st[1];
    var current = props.value !== undefined ? props.value : val;
    var labelId = name + '-l';
    return h('div', { role: 'radiogroup', 'aria-labelledby': props.label ? labelId : undefined, 'aria-disabled': props.isDisabled || undefined, className: cx('lm-root lm-radiogroup', props.orientation === 'horizontal' && 'lm-radiogroup--horizontal') },
      props.label ? h('span', { id: labelId, className: 'lm-label lm-radiogroup__label' }, props.label) : null,
      h('div', { className: cx('lm-radiogroup__items', props.orientation === 'horizontal' && 'lm-radiogroup__items--horizontal') },
      props.options.map(function (o) {
        return h('label', { key: o.value, className: cx('lm-radio', (props.isDisabled || o.isDisabled) && 'lm-radio--disabled') },
          h('input', { type: 'radio', name: name, value: o.value, checked: current === o.value, disabled: props.isDisabled || o.isDisabled,
            onChange: function () { setVal(o.value); if (props.onChange) props.onChange(o.value); } }),
          h('span', { className: 'lm-radio__dot', 'aria-hidden': 'true' }), o.label);
      })));
  }

  /* Tabs */
  function Tabs(props) {
    var id = useId();
    var st = useState(props.defaultSelectedKey || props.items[0].key), sel = st[0], setSel = st[1];
    var current = props.selectedKey !== undefined ? props.selectedKey : sel;
    function onKey(e, i) {
      var n = props.items.length, next = null;
      if (e.key === 'ArrowRight') next = (i + 1) % n; else if (e.key === 'ArrowLeft') next = (i - 1 + n) % n;
      if (next === null) return; e.preventDefault();
      var k = props.items[next].key; setSel(k); if (props.onSelectionChange) props.onSelectionChange(k);
      var btn = document.getElementById(id + '-t-' + k); if (btn) btn.focus();
    }
    var item = props.items.filter(function (t) { return t.key === current; })[0];
    return h('div', { className: 'lm-root' },
      h('div', { className: 'lm-tabs', role: 'tablist', 'aria-label': props['aria-label'] },
        props.items.map(function (t, i) {
          return h('button', { key: t.key, id: id + '-t-' + t.key, role: 'tab', type: 'button', className: 'lm-focusable lm-tab',
            'aria-selected': t.key === current, 'aria-controls': id + '-p', tabIndex: t.key === current ? 0 : -1, disabled: t.isDisabled,
            onClick: function () { setSel(t.key); if (props.onSelectionChange) props.onSelectionChange(t.key); },
            onKeyDown: function (e) { onKey(e, i); } }, t.title);
        })),
      h('div', { id: id + '-p', role: 'tabpanel', className: 'lm-tabpanel', 'aria-labelledby': id + '-t-' + current }, item ? item.content : null));
  }

  /* StatusLight */
  function StatusLight(props) {
    return h('span', { className: cx('lm-root lm-status', 'lm-status--' + (props.variant || 'neutral')) },
      h('span', { className: 'lm-status__dot', 'aria-hidden': 'true' }), props.children);
  }

  /* Badge */
  function Badge(props) {
    var v = props.variant || 'neutral';
    return h('span', { className: cx('lm-root lm-badge', 'lm-badge--' + v) },
      v !== 'neutral' ? h(StatusGlyph, { name: v === 'informative' ? 'info' : v, size: 12 }) : null, props.children);
  }

  /* ProgressBar */
  function ProgressBar(props) {
    var det = typeof props.value === 'number';
    var pct = det ? Math.max(0, Math.min(100, props.value)) : null;
    return h('div', { className: 'lm-root lm-progress' },
      h('div', { className: 'lm-progress__head' }, h('span', null, props.label), det ? h('span', null, pct + '%') : null),
      h('div', { className: 'lm-progress__track', role: 'progressbar', 'aria-label': props.label, 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-valuenow': det ? pct : undefined },
        h('div', { className: cx('lm-progress__fill', !det && 'lm-progress__fill--indeterminate'), style: det ? { width: pct + '%' } : null })));
  }

  /* InlineAlert */
  function InlineAlert(props) {
    var v = props.variant || 'neutral';
    return h('div', { className: cx('lm-root lm-alert', 'lm-alert--' + v), role: v === 'negative' ? 'alert' : 'status' },
      h(StatusGlyph, { name: v === 'informative' || v === 'neutral' ? 'info' : v, size: 18, className: 'lm-alert__icon' }),
      h('div', null, props.title ? h('p', { className: 'lm-alert__title' }, props.title) : null, h('p', { className: 'lm-alert__body' }, props.children)));
  }

  /* Divider */
  function Divider(props) {
    return h('hr', { className: cx('lm-root lm-divider', props.size === 'm' && 'lm-divider--m', props.size === 'l' && 'lm-divider--l', props.orientation === 'vertical' && 'lm-divider--vertical'),
      'aria-orientation': props.orientation === 'vertical' ? 'vertical' : undefined });
  }

  /* Card */
  function Card(props) {
    var interactive = typeof props.onPress === 'function';
    var level = 'h' + (props.headingLevel || 3);
    var common = {
      className: cx('lm-root lm-card', props.orientation === 'horizontal' && 'lm-card--horizontal', interactive && 'lm-card--interactive lm-focusable', props.isSelected && 'lm-card--selected', props.className),
      style: props.width ? { width: props.width } : null
    };
    if (interactive) {
      common.role = 'button'; common.tabIndex = 0; common.onClick = props.onPress;
      if (props.isSelected !== undefined) common['aria-pressed'] = !!props.isSelected;
      common.onKeyDown = function (e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); props.onPress(e); } };
    }
    return h('div', common,
      props.preview ? h('div', { className: 'lm-card__preview' }, props.preview) : null,
      h('div', { className: 'lm-card__body' },
        props.title ? h(level, { className: 'lm-card__title' }, props.title) : null,
        props.description ? h('p', { className: 'lm-card__desc' }, props.description) : null,
        props.children,
        props.footer ? h('div', { className: 'lm-card__footer' }, props.footer) : null),
      props.isSelected ? h(StatusGlyph, { name: 'positive', size: 18, className: 'lm-card__check' }) : null);
  }

  /* Table */
  function toggleKey(list, key) { return list.indexOf(key) > -1 ? list.filter(function (k) { return k !== key; }) : list.concat([key]); }
  function Table(props) {
    var cols = props.columns, density = props.density || 'regular', multi = props.selectionMode === 'multiple';
    var sortSt = useState(props.defaultSortDescriptor || null), sortInt = sortSt[0], setSort = sortSt[1];
    var sort = props.sortDescriptor !== undefined ? props.sortDescriptor : sortInt;
    var selSt = useState(props.defaultSelectedKeys || []), selInt = selSt[0], setSel = selSt[1];
    var selected = props.selectedKeys !== undefined ? props.selectedKeys : selInt;
    var rows = props.rows.slice();
    if (sort && props.sortDescriptor === undefined) {
      rows.sort(function (a, b) {
        var x = a[sort.column], y = b[sort.column];
        var r = (typeof x === 'number' && typeof y === 'number') ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true });
        return sort.direction === 'descending' ? -r : r;
      });
    }
    function changeSel(next) { setSel(next); if (props.onSelectionChange) props.onSelectionChange(next); }
    function changeSort(col) {
      var dir = sort && sort.column === col && sort.direction === 'ascending' ? 'descending' : 'ascending';
      var next = { column: col, direction: dir }; setSort(next); if (props.onSortChange) props.onSortChange(next);
    }
    var allKeys = rows.map(function (r) { return r.key; });
    var allOn = allKeys.length > 0 && allKeys.every(function (k) { return selected.indexOf(k) > -1; });
    var someOn = !allOn && selected.length > 0;
    return h('div', { className: 'lm-root lm-table-wrap' },
      h('table', { className: cx('lm-table', 'lm-table--' + density), 'aria-label': props['aria-label'], 'aria-multiselectable': multi || undefined },
        h('thead', null, h('tr', null,
          multi ? h('th', { scope: 'col', className: 'lm-table__select' }, h(Checkbox, { 'aria-label': 'Select all rows', isSelected: allOn, isIndeterminate: someOn, onChange: function (on) { changeSel(on ? allKeys : []); } })) : null,
          cols.map(function (c) {
            var isSorted = sort && sort.column === c.key;
            var inner = c.allowsSorting
              ? h('button', { type: 'button', className: 'lm-focusable lm-table__sort', onClick: function () { changeSort(c.key); } }, c.title,
                  h('span', { className: 'lm-table__arrowwrap', style: { opacity: isSorted ? 1 : 0.35, display: 'inline-flex' } }, h(Icon, { name: 'arrow-up', size: 14, className: cx('lm-table__arrow', isSorted && sort.direction === 'descending' && 'lm-table__arrow--desc'), strokeWidth: 2.6 })))
              : c.title;
            return h('th', { key: c.key, scope: 'col', style: { textAlign: c.align || 'start', width: c.width }, 'aria-sort': c.allowsSorting ? (isSorted ? sort.direction : 'none') : undefined }, inner);
          }))),
        h('tbody', null,
          rows.length === 0 && props.emptyState ? h('tr', null, h('td', { colSpan: cols.length + (multi ? 1 : 0), className: 'lm-table__empty' }, props.emptyState)) : null,
          rows.map(function (r) {
            var on = selected.indexOf(r.key) > -1;
            return h('tr', { key: r.key, 'aria-selected': multi ? on : undefined, className: on ? 'lm-table__row--selected' : null },
              multi ? h('td', { className: 'lm-table__select' }, h(Checkbox, { 'aria-label': 'Select ' + (r.label || r.key), isSelected: on, onChange: function () { changeSel(toggleKey(selected, r.key)); } })) : null,
              cols.map(function (c) {
                var content = c.render ? c.render(r) : r[c.key];
                var st = { textAlign: c.align || 'start' };
                return c.isRowHeader ? h('th', { key: c.key, scope: 'row', style: st, className: 'lm-table__rowheader' }, content) : h('td', { key: c.key, style: st }, content);
              }));
          }))));
  }

  /* List */
  function List(props) {
    var mode = props.selectionMode || 'none', items = props.items;
    var selSt = useState(props.defaultSelectedKeys || []), selInt = selSt[0], setSel = selSt[1];
    var selected = props.selectedKeys !== undefined ? props.selectedKeys : selInt;
    var fSt = useState(0), focusIdx = fSt[0], setFocus = fSt[1];
    var refs = React.useRef([]);
    function choose(key) {
      var next = mode === 'single' ? [key] : toggleKey(selected, key);
      setSel(next); if (props.onSelectionChange) props.onSelectionChange(next);
    }
    function onKey(e, i, key) {
      var n = items.length, to = null;
      if (e.key === 'ArrowDown') to = (i + 1) % n; else if (e.key === 'ArrowUp') to = (i - 1 + n) % n;
      if (e.key === 'Home') to = 0; if (e.key === 'End') to = n - 1;
      if (to !== null) { e.preventDefault(); setFocus(to); if (refs.current[to]) refs.current[to].focus(); }
      else if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); choose(key); }
    }
    function body(it) {
      return [
        it.visual ? h('span', { key: 'v', className: 'lm-list__visual' }, it.visual) : null,
        h('span', { key: 't', className: 'lm-list__text' }, h('span', { className: 'lm-list__title' }, it.title), it.description ? h('span', { className: 'lm-list__desc' }, it.description) : null),
        it.end ? h('span', { key: 'e', className: 'lm-list__end' }, it.end) : null
      ];
    }
    var selectable = mode !== 'none';
    return h('ul', { className: cx('lm-root lm-list', props.className), role: selectable ? 'listbox' : undefined, 'aria-multiselectable': mode === 'multiple' || undefined, 'aria-label': props['aria-label'] },
      items.map(function (it, i) {
        if (selectable) {
          var on = selected.indexOf(it.key) > -1;
          return h('li', { key: it.key, role: 'option', 'aria-selected': on, ref: function (el) { refs.current[i] = el; }, tabIndex: i === focusIdx ? 0 : -1,
            className: cx('lm-list__item lm-focusable lm-list__item--action', on && 'lm-list__item--selected'),
            onClick: function () { setFocus(i); choose(it.key); }, onKeyDown: function (e) { onKey(e, i, it.key); } },
            body(it), on ? h(StatusGlyph, { name: 'positive', size: 18, className: 'lm-list__check' }) : null);
        }
        if (props.onAction) {
          return h('li', { key: it.key, className: 'lm-list__li' }, h('button', { type: 'button', className: 'lm-list__item lm-list__item--action lm-focusable', onClick: function () { props.onAction(it.key); } }, body(it)));
        }
        return h('li', { key: it.key, className: 'lm-list__item' }, body(it));
      }));
  }

  /* Disclosure + Accordion */
  function Chevron() { return h(Icon, { name: 'chevron-down', size: 16, className: 'lm-disc__chev', strokeWidth: 2.6 }); }
  function DisclosureItem(props) {
    var id = useId();
    return h('div', { className: cx('lm-disc', props.isExpanded && 'lm-disc--open') },
      h('h3', { className: 'lm-disc__heading' }, h('button', { type: 'button', id: id + '-b', className: 'lm-focusable lm-disc__trigger', 'aria-expanded': !!props.isExpanded, 'aria-controls': id + '-p', disabled: props.isDisabled, onClick: props.onToggle }, h(Chevron), props.title)),
      h('div', { id: id + '-p', role: 'region', 'aria-labelledby': id + '-b', className: 'lm-disc__panel', hidden: !props.isExpanded }, props.children));
  }
  function Disclosure(props) {
    var st = useState(!!props.defaultExpanded), inner = st[0], setInner = st[1];
    var open = props.isExpanded !== undefined ? props.isExpanded : inner;
    return h('div', { className: cx('lm-root lm-disclosure', props.className) },
      h(DisclosureItem, { title: props.title, isExpanded: open, isDisabled: props.isDisabled, onToggle: function () { setInner(!open); if (props.onExpandedChange) props.onExpandedChange(!open); } }, props.children));
  }
  function Accordion(props) {
    var multi = !!props.allowsMultipleExpanded;
    var st = useState(props.defaultExpandedKeys || []), inner = st[0], setInner = st[1];
    var open = props.expandedKeys !== undefined ? props.expandedKeys : inner;
    function toggle(key) {
      var next = open.indexOf(key) > -1 ? open.filter(function (k) { return k !== key; }) : (multi ? open.concat([key]) : [key]);
      setInner(next); if (props.onExpandedChange) props.onExpandedChange(next);
    }
    return h('div', { className: cx('lm-root lm-accordion', props.className) },
      props.items.map(function (it) {
        return h(DisclosureItem, { key: it.key, title: it.title, isDisabled: it.isDisabled, isExpanded: open.indexOf(it.key) > -1, onToggle: function () { toggle(it.key); } }, it.content);
      }));
  }

  /* Tree */
  function Tree(props) {
    var mode = props.selectionMode || 'single', items = props.items;
    var exSt = useState(props.defaultExpandedKeys || []), expanded = props.expandedKeys !== undefined ? props.expandedKeys : exSt[0], setEx = exSt[1];
    var selSt = useState(props.defaultSelectedKey || null), selInt = selSt[0], setSel = selSt[1];
    var selected = props.selectedKey !== undefined ? props.selectedKey : selInt;
    var focSt = useState(null), focus = focSt[0], setFocus = focSt[1];
    var refs = React.useRef({});
    var vis = [];
    (function walk(list, level, parent) {
      list.forEach(function (it) {
        var has = !!(it.children && it.children.length);
        vis.push({ key: it.key, level: level, parent: parent, has: has });
        if (has && expanded.indexOf(it.key) > -1) walk(it.children, level + 1, it.key);
      });
    })(items, 1, null);
    var tabKey = vis.some(function (v) { return v.key === focus; }) ? focus : (vis.some(function (v) { return v.key === selected; }) ? selected : vis[0] && vis[0].key);
    function toggle(key) {
      var next = expanded.indexOf(key) > -1 ? expanded.filter(function (k) { return k !== key; }) : expanded.concat([key]);
      setEx(next); if (props.onExpandedChange) props.onExpandedChange(next);
    }
    function select(key) { setSel(key); if (props.onSelectionChange) props.onSelectionChange(key); }
    function go(key) { if (key == null) return; setFocus(key); if (refs.current[key]) refs.current[key].focus(); }
    function onKey(e, key) {
      e.stopPropagation();
      var i = vis.map(function (v) { return v.key; }).indexOf(key), v = vis[i], open = expanded.indexOf(key) > -1, k = e.key, handled = true;
      if (k === 'ArrowDown') go(vis[i + 1] && vis[i + 1].key);
      else if (k === 'ArrowUp') go(vis[i - 1] && vis[i - 1].key);
      else if (k === 'Home') go(vis[0].key);
      else if (k === 'End') go(vis[vis.length - 1].key);
      else if (k === 'ArrowRight') { if (v.has && !open) toggle(key); else if (v.has && open) go(vis[i + 1].key); }
      else if (k === 'ArrowLeft') { if (v.has && open) toggle(key); else if (v.parent) go(v.parent); }
      else if (k === 'Enter' || k === ' ') { if (mode !== 'none') select(key); else if (v.has) toggle(key); }
      else handled = false;
      if (handled) e.preventDefault();
    }
    function render(list, level) {
      return list.map(function (it) {
        var has = !!(it.children && it.children.length), open = expanded.indexOf(it.key) > -1, on = mode !== 'none' && selected === it.key;
        return h('li', { key: it.key, role: 'treeitem', 'aria-level': level, 'aria-expanded': has ? open : undefined, 'aria-selected': mode !== 'none' ? on : undefined,
          tabIndex: it.key === tabKey ? 0 : -1, ref: function (el) { refs.current[it.key] = el; }, className: 'lm-tree__item', onKeyDown: function (e) { onKey(e, it.key); }, onFocus: function (e) { if (e.target === e.currentTarget) setFocus(it.key); } },
          h('div', { className: cx('lm-tree__row', on && 'lm-tree__row--selected'), style: { paddingInlineStart: 8 + (level - 1) * 20 },
            onClick: function () { setFocus(it.key); if (mode !== 'none') select(it.key); else if (has) toggle(it.key); } },
            has ? h('span', { className: cx('lm-tree__chev', open && 'lm-tree__chev--open'), onClick: function (e) { e.stopPropagation(); toggle(it.key); } }, h(Chevron)) : h('span', { className: 'lm-tree__chev lm-tree__chev--none' }),
            it.visual ? h('span', { className: 'lm-tree__visual' }, it.visual) : null,
            h('span', { className: 'lm-tree__title' }, it.title),
            on ? h(StatusGlyph, { name: 'positive', size: 14, className: 'lm-tree__check' }) : null),
          has && open ? h('ul', { role: 'group', className: 'lm-tree__group' }, render(it.children, level + 1)) : null);
      });
    }
    return h('ul', { role: 'tree', 'aria-label': props['aria-label'], 'aria-multiselectable': undefined, className: cx('lm-root lm-tree', props.className) }, render(items, 1));
  }

  /* Pagination */
  function pageItems(page, count, sib) {
    var out = [], i;
    if (count <= 5 + 2 * sib) { for (i = 1; i <= count; i++) out.push(i); return out; }
    var left = Math.max(page - sib, 2), right = Math.min(page + sib, count - 1);
    if (page <= sib + 3) { left = 2; right = 2 * sib + 3; }
    else if (page >= count - sib - 2) { right = count - 1; left = count - (2 * sib + 2); }
    out.push(1); if (left > 2) out.push('gap-l');
    for (i = left; i <= right; i++) out.push(i);
    if (right < count - 1) out.push('gap-r'); out.push(count);
    return out;
  }
  function Pagination(props) {
    var st = useState(props.defaultPage || 1), inner = st[0], setInner = st[1];
    var page = props.page !== undefined ? props.page : inner, count = props.pageCount;
    function go(p) { if (p < 1 || p > count || p === page) return; setInner(p); if (props.onChange) props.onChange(p); }
    return h('nav', { className: cx('lm-root lm-pagination', props.className), 'aria-label': props['aria-label'] || 'Pagination' },
      h('button', { type: 'button', className: 'lm-focusable lm-page lm-page--nav', 'aria-label': 'Previous page', disabled: page <= 1, onClick: function () { go(page - 1); } }, h(Icon, { name: 'chevron-left', size: 16, strokeWidth: 2.6 })),
      pageItems(page, count, props.siblingCount || 1).map(function (p) {
        if (typeof p === 'string') return h('span', { key: p, className: 'lm-page lm-page--gap', 'aria-hidden': 'true' }, '…');
        return h('button', { key: p, type: 'button', className: cx('lm-focusable lm-page', p === page && 'lm-page--current'), 'aria-label': 'Page ' + p, 'aria-current': p === page ? 'page' : undefined, onClick: function () { go(p); } }, p);
      }),
      h('button', { type: 'button', className: 'lm-focusable lm-page lm-page--nav', 'aria-label': 'Next page', disabled: page >= count, onClick: function () { go(page + 1); } }, h(Icon, { name: 'chevron-right', size: 16, strokeWidth: 2.6 })));
  }

  /* Avatar + AvatarGroup */
  function initials(name) { var p = String(name || '').trim().split(/\s+/); return ((p[0] || '').charAt(0) + (p.length > 1 ? p[p.length - 1].charAt(0) : '')).toUpperCase(); }
  function Avatar(props) {
    var size = props.size || 'm';
    return h('span', { className: cx('lm-root lm-avatar', 'lm-avatar--' + size, props.isDisabled && 'lm-avatar--disabled', props.className), role: 'img', 'aria-label': props.name || props.alt },
      props.src ? h('img', { src: props.src, alt: '', className: 'lm-avatar__img' }) : h('span', { 'aria-hidden': 'true' }, props.children || initials(props.name)));
  }
  function AvatarGroup(props) {
    var max = props.max || 4, list = props.avatars, shown = list.slice(0, max), extra = list.length - shown.length;
    return h('div', { className: cx('lm-root lm-avatargroup', 'lm-avatargroup--' + (props.size || 'm'), props.className), role: 'group', 'aria-label': props['aria-label'] || (list.length + ' people') },
      shown.map(function (a, i) { return h(Avatar, { key: i, name: a.name, src: a.src, size: props.size }); }),
      extra > 0 ? h(Avatar, { size: props.size, name: extra + ' more' }, '+' + extra) : null);
  }

  /* Dropdown (select) + Menu (actions) */
  function PickerBase(props) {
    var mode = props.mode, items = props.items, id = useId();
    var openSt = useState(!!props.defaultOpen), open = openSt[0], setOpen = openSt[1];
    var selSt = useState(props.defaultSelectedKey || null), selInt = selSt[0], setSel = selSt[1];
    var actSt = useState(0), active = actSt[0], setActive = actSt[1];
    var selected = props.selectedKey !== undefined ? props.selectedKey : selInt;
    var wrap = React.useRef(), list = React.useRef(), btn = React.useRef();
    var selItem = items.filter(function (it) { return it.key === selected; })[0];
    function firstEnabled(from, dir) { var n = items.length, i = from; for (var c = 0; c < n; c++) { if (i >= 0 && i < n && !items[i].isDisabled) return i; i += dir; } return from; }
    function openIt() {
      if (props.isDisabled) return;
      var si = items.map(function (it) { return it.key; }).indexOf(selected);
      setActive(si > -1 ? si : firstEnabled(0, 1)); setOpen(true);
    }
    function close(refocus) { setOpen(false); if (refocus && btn.current) btn.current.focus(); }
    React.useEffect(function () { if (open && list.current && !props.defaultOpen) list.current.focus({ preventScroll: true }); }, [open]);
    function choose(i) {
      var it = items[i]; if (!it || it.isDisabled) return;
      if (mode === 'select') { setSel(it.key); if (props.onSelectionChange) props.onSelectionChange(it.key); }
      else if (props.onAction) props.onAction(it.key);
      close(true);
    }
    function step(dir) { var n = items.length, i = active; do { i = (i + dir + n) % n; } while (items[i].isDisabled && i !== active); setActive(i); }
    function onListKey(e) {
      var k = e.key;
      if (k === 'ArrowDown') { e.preventDefault(); step(1); } else if (k === 'ArrowUp') { e.preventDefault(); step(-1); }
      else if (k === 'Home') { e.preventDefault(); setActive(firstEnabled(0, 1)); } else if (k === 'End') { e.preventDefault(); setActive(firstEnabled(items.length - 1, -1)); }
      else if (k === 'Enter' || k === ' ') { e.preventDefault(); choose(active); }
      else if (k === 'Escape') { e.preventDefault(); close(true); } else if (k === 'Tab') close(false);
    }
    function onBlur(e) { if (open && wrap.current && !wrap.current.contains(e.relatedTarget)) setOpen(false); }
    var labelId = id + '-label', btnId = id + '-btn', listId = id + '-list';
    var isMenu = mode === 'menu', invalid = props.validationState === 'invalid', help = invalid ? props.errorMessage : props.description;
    return h('div', { className: cx('lm-root lm-picker', isMenu && 'lm-picker--menu', props.className), style: props.width ? { width: props.width } : null },
      !isMenu && props.label ? h('span', { id: labelId, className: 'lm-label' }, props.label) : null,
      h('div', { className: 'lm-picker__wrap', ref: wrap, onBlur: onBlur },
        h('button', { type: 'button', id: btnId, ref: btn, className: cx('lm-focusable', isMenu ? 'lm-action' : 'lm-picker__btn'), 'aria-haspopup': isMenu ? 'menu' : 'listbox', 'aria-expanded': open, 'aria-controls': open ? listId : undefined,
          'aria-labelledby': !isMenu && props.label ? labelId + ' ' + btnId : undefined, 'aria-invalid': invalid || undefined, disabled: props.isDisabled,
          onClick: function () { open ? close(false) : openIt(); }, onKeyDown: function (e) { if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); openIt(); } } },
          isMenu ? props.label : h('span', { className: cx('lm-picker__value', !selItem && 'lm-picker__value--placeholder') }, selItem ? selItem.label : (props.placeholder || 'Select an option')),
          h(Chevron)),
        open ? h('ul', { id: listId, ref: list, tabIndex: -1, role: isMenu ? 'menu' : 'listbox', 'aria-labelledby': isMenu ? btnId : labelId, 'aria-activedescendant': id + '-o' + active, className: 'lm-popover', onKeyDown: onListKey },
          items.map(function (it, i) {
            var on = !isMenu && it.key === selected;
            return h('li', { key: it.key, id: id + '-o' + i, role: isMenu ? 'menuitem' : 'option', 'aria-selected': isMenu ? undefined : on, 'aria-disabled': it.isDisabled || undefined,
              className: cx('lm-popover__item', i === active && 'lm-popover__item--active', on && 'lm-popover__item--selected', it.isDisabled && 'lm-popover__item--disabled', it.isDestructive && 'lm-popover__item--danger'),
              onMouseEnter: function () { if (!it.isDisabled) setActive(i); }, onMouseDown: function (e) { e.preventDefault(); }, onClick: function () { choose(i); } },
              h('span', { className: 'lm-popover__text' }, h('span', null, it.label), it.description ? h('span', { className: 'lm-popover__desc' }, it.description) : null),
              on ? h(StatusGlyph, { name: 'positive', size: 14, className: 'lm-popover__check' }) : null);
          })) : null),
      help && !isMenu ? h('div', { className: cx('lm-help', invalid && 'lm-help--error') }, invalid ? h(StatusGlyph, { name: 'negative', size: 14 }) : null, h('span', null, help)) : null);
  }
  function Dropdown(props) { return h(PickerBase, Object.assign({}, props, { mode: 'select' })); }
  function Menu(props) { return h(PickerBase, Object.assign({}, props, { mode: 'menu' })); }

  /* Steps */
  function Steps(props) {
    var cur = props.currentStep || 0, vertical = props.orientation === 'vertical';
    return h('ol', { className: cx('lm-root lm-steps', vertical && 'lm-steps--vertical', props.className), 'aria-label': props['aria-label'] },
      props.items.map(function (it, i) {
        var state = i < cur ? 'done' : (i === cur ? 'current' : 'todo');
        var clickable = typeof props.onStepPress === 'function' && state === 'done';
        var marker = h('span', { className: 'lm-steps__marker', 'aria-hidden': 'true' }, state === 'done' ? h(Icon, { name: 'check', size: 10, strokeWidth: 4 }) : null);
        var text = h('span', { className: 'lm-steps__text' }, h('span', { className: 'lm-steps__title' }, it.title), it.description ? h('span', { className: 'lm-steps__desc' }, it.description) : null,
          h('span', { className: 'lm-sr' }, state === 'done' ? ', completed' : (state === 'current' ? ', current step' : ', not started')));
        return h('li', { key: it.key || i, className: cx('lm-steps__item', 'lm-steps__item--' + state), 'aria-current': state === 'current' ? 'step' : undefined },
          clickable ? h('button', { type: 'button', className: 'lm-focusable lm-steps__btn', onClick: function () { props.onStepPress(i); } }, marker, text) : h('span', { className: 'lm-steps__btn' }, marker, text));
      }));
  }

  /* Timeline */
  function Timeline(props) {
    return h('ol', { className: cx('lm-root lm-timeline', props.className), 'aria-label': props['aria-label'] },
      props.items.map(function (it, i) {
        return h('li', { key: it.key || i, className: cx('lm-timeline__item', 'lm-timeline__item--' + (it.variant || 'neutral')) },
          h('span', { className: 'lm-timeline__dot', 'aria-hidden': 'true' }),
          h('div', { className: 'lm-timeline__body' },
            h('div', { className: 'lm-timeline__head' }, h('span', { className: 'lm-timeline__title' }, it.title), it.time ? h('time', { className: 'lm-timeline__time', dateTime: it.dateTime }, it.time) : null),
            it.description ? h('p', { className: 'lm-timeline__desc' }, it.description) : null));
      }));
  }

  /* Drawer */
  function DrawerPanel(props) {
    var id = useId(), panel = React.useRef(), side = props.side || 'right';
    React.useEffect(function () {
      var prev = document.activeElement;
      if (panel.current) panel.current.focus({ preventScroll: true });
      return function () { if (prev && prev.focus) prev.focus({ preventScroll: true }); };
    }, []);
    function onKey(e) {
      if (e.key === 'Escape') { e.stopPropagation(); props.onClose(); return; }
      if (e.key !== 'Tab' || !panel.current) return;
      var f = panel.current.querySelectorAll('button:not([disabled]), [href], input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])');
      if (!f.length) { e.preventDefault(); return; }
      var first = f[0], last = f[f.length - 1];
      if (e.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
    return h('div', { className: cx('lm-root lm-drawer-wrap', props.isContained && 'lm-drawer-wrap--contained') },
      h('div', { className: 'lm-drawer__scrim', onClick: props.onClose }),
      h('aside', { ref: panel, role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': id, tabIndex: -1, className: cx('lm-drawer', 'lm-drawer--' + side), style: { width: props.width || 360 }, onKeyDown: onKey },
        h('header', { className: 'lm-drawer__header' }, h('h2', { id: id, className: 'lm-drawer__title' }, props.title),
          h('button', { type: 'button', className: 'lm-focusable lm-action lm-action--quiet lm-drawer__close', 'aria-label': 'Close', onClick: props.onClose }, h(Icon, { name: 'close', size: 16, strokeWidth: 2.6 }))),
        h('div', { className: 'lm-drawer__body' }, props.children),
        props.footer ? h('footer', { className: 'lm-drawer__footer' }, props.footer) : null));
  }
  function Drawer(props) { return props.isOpen ? h(DrawerPanel, props) : null; }

  /* EmptyState */
  function Illustration(props) {
    if (props.name === 'search') return h('svg', { width: 120, height: 96, viewBox: '0 0 120 96', 'aria-hidden': 'true', className: 'lm-ill' },
      h('rect', { x: 8, y: 16, width: 104, height: 64, rx: 8, className: 'lm-ill__panel' }), h('rect', { x: 20, y: 28, width: 40, height: 8, rx: 4, className: 'lm-ill__bar' }), h('rect', { x: 20, y: 44, width: 56, height: 8, rx: 4, className: 'lm-ill__bar' }),
      h('circle', { cx: 80, cy: 54, r: 18, className: 'lm-ill__lens' }), h('path', { d: 'm93 67 14 14', className: 'lm-ill__handle' }));
    return h('svg', { width: 120, height: 96, viewBox: '0 0 120 96', 'aria-hidden': 'true', className: 'lm-ill' },
      h('rect', { x: 20, y: 10, width: 80, height: 56, rx: 8, className: 'lm-ill__bar' }),
      h('rect', { x: 12, y: 22, width: 96, height: 56, rx: 8, className: 'lm-ill__panel' }),
      h('circle', { cx: 60, cy: 50, r: 10, className: 'lm-ill__dot' }));
  }
  function EmptyState(props) {
    var level = 'h' + (props.headingLevel || 2);
    return h('div', { className: cx('lm-root lm-empty', props.className) },
      props.illustration === null ? null : (typeof props.illustration === 'string' || props.illustration === undefined ? h(Illustration, { name: props.illustration || 'empty' }) : props.illustration),
      h(level, { className: 'lm-empty__title' }, props.title),
      props.description ? h('p', { className: 'lm-empty__desc' }, props.description) : null,
      props.children ? h('div', { className: 'lm-empty__actions' }, props.children) : null);
  }

  /* LoginForm */
  var EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  function LoginForm(props) {
    var vSt = useState({ email: props.defaultEmail || '', password: '', remember: false }), v = vSt[0], setV = vSt[1];
    var eSt = useState({}), errs = eSt[0], setErrs = eSt[1];
    var emailRef = React.useRef(), pwRef = React.useRef();
    function set(k, val) { var n = Object.assign({}, v); n[k] = val; setV(n); if (errs[k]) { var e = Object.assign({}, errs); delete e[k]; setErrs(e); } }
    function submit(ev) {
      ev.preventDefault();
      var e = {};
      if (!v.email.trim()) e.email = 'Enter your email address.'; else if (!EMAIL_RE.test(v.email.trim())) e.email = 'Enter a complete email address, like name@company.com.';
      if (!v.password) e.password = 'Enter your password.';
      setErrs(e);
      if (e.email) { if (emailRef.current) emailRef.current.focus(); return; }
      if (e.password) { if (pwRef.current) pwRef.current.focus(); return; }
      if (props.onSubmit) props.onSubmit({ email: v.email.trim(), password: v.password, remember: v.remember });
    }
    var titleId = useId();
    return h('form', { className: cx('lm-root lm-login', props.className), noValidate: true, onSubmit: submit, 'aria-labelledby': titleId },
      props.logo ? h('div', { className: 'lm-login__logo' }, props.logo) : null,
      h('div', { className: 'lm-login__head' }, h('h1', { id: titleId, className: 'lm-login__title' }, props.title || 'Sign in'), props.description ? h('p', { className: 'lm-login__desc' }, props.description) : null),
      props.errorMessage ? h(InlineAlert, { variant: 'negative', title: props.errorTitle || 'Could not sign in' }, props.errorMessage) : null,
      h(TextField, { label: 'Email', type: 'email', name: 'email', autoComplete: 'username', width: '100%', value: v.email, inputRef: emailRef, isRequired: true, isDisabled: props.isLoading, placeholder: 'name@company.com',
        validationState: errs.email ? 'invalid' : undefined, errorMessage: errs.email, onChange: function (x) { set('email', x); } }),
      h(TextField, { label: 'Password', type: 'password', name: 'password', autoComplete: 'current-password', width: '100%', value: v.password, inputRef: pwRef, isRequired: true, isDisabled: props.isLoading, revealable: true,
        validationState: errs.password ? 'invalid' : undefined, errorMessage: errs.password, onChange: function (x) { set('password', x); } }),
      h('div', { className: 'lm-login__row' },
        h(Checkbox, { isSelected: v.remember, isDisabled: props.isLoading, onChange: function (x) { set('remember', x); } }, 'Keep me signed in'),
        h('a', { className: 'lm-link lm-focusable', href: props.forgotPasswordHref || '#' }, 'Forgot password?')),
      h(Button, { type: 'submit', className: 'lm-button--block', isDisabled: props.isLoading }, props.isLoading ? 'Signing in…' : (props.submitLabel || 'Sign in')),
      props.onSSO ? [h('div', { key: 'or', className: 'lm-login__or', role: 'separator' }, h('span', null, 'or')), h(Button, { key: 'sso', variant: 'secondary', outline: true, className: 'lm-button--block', isDisabled: props.isLoading, onPress: props.onSSO }, props.ssoLabel || 'Continue with single sign-on')] : null,
      props.signUpHref ? h('p', { className: 'lm-login__foot' }, 'New here? ', h('a', { className: 'lm-link lm-focusable', href: props.signUpHref }, 'Create an account')) : null);
  }

  window.Lumen = { Button: Button, ActionButton: ActionButton, TextField: TextField, SearchField: SearchField, TextArea: TextArea, Checkbox: Checkbox, Switch: Switch, RadioGroup: RadioGroup, Tabs: Tabs, StatusLight: StatusLight, Badge: Badge, ProgressBar: ProgressBar, InlineAlert: InlineAlert, Divider: Divider, Card: Card, Table: Table, List: List, Disclosure: Disclosure, Accordion: Accordion, Tree: Tree, Pagination: Pagination, Avatar: Avatar, AvatarGroup: AvatarGroup, Dropdown: Dropdown, Menu: Menu, Steps: Steps, Timeline: Timeline, Drawer: Drawer, EmptyState: EmptyState, LoginForm: LoginForm, Icon: Icon, iconNames: ICON_NAMES };
})();
