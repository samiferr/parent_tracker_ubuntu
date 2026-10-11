import { h, useState, useRef, L } from '../h.js';
import { api } from '../api.js';

const { TextField, Button, InlineAlert } = L;

// Sign-in is composed from TextField, Button and InlineAlert, following
// LoginForm's rules, because LoginForm validates its first field as an email
// address and the dashboard user name (default "parent") is not one (docs/12 §12.6):
// visible labels, validation on submit, focus on the first invalid field, the
// user name kept after a failure, and a vague server error.
export function SignIn({ configured, onSignedIn }) {
  const [username, setUsername] = useState('parent');
  const [password, setPassword] = useState('');
  const [errors, setErrors] = useState({});
  const [serverError, setServerError] = useState(null);
  const [busy, setBusy] = useState(false);
  const userRef = useRef(null);
  const passRef = useRef(null);

  async function submit(e) {
    if (e && e.preventDefault) e.preventDefault();
    const next = {};
    if (!username.trim()) next.username = 'Enter your username.';
    if (!password) next.password = 'Enter your password.';
    setErrors(next);
    setServerError(null);
    if (next.username) { userRef.current && userRef.current.focus(); return; }
    if (next.password) { passRef.current && passRef.current.focus(); return; }
    setBusy(true);
    try {
      await api.post('/api/login', { username: username.trim(), password });
      onSignedIn();
    } catch (err) {
      setPassword(''); // keep the user name, clear the password
      setServerError(err);
      if (passRef.current) passRef.current.focus();
    } finally {
      setBusy(false);
    }
  }

  return h('main', { className: 'signin' },
    h('form', { className: 'signin__card', onSubmit: submit, noValidate: true, 'aria-labelledby': 'signin-title' },
      h('div', { className: 'stack stack--tight' },
        h('h1', { id: 'signin-title', className: 'heading-m hero' }, 'Sign in'),
        h('p', { className: 'muted body-s' }, 'Parent Tracker shows how this PC is used. Sign in with the login you chose during setup.')),
      !configured ? h(InlineAlert, { variant: 'notice', title: 'Sign-in is not set up yet.' }, 'Run sudo parent-tracker setup on the PC to choose a password.') : null,
      serverError ? h(InlineAlert, { variant: serverError.status === 429 ? 'notice' : 'negative', title: serverError.message },
        serverError.hint || null) : null,
      h('div', { className: 'signin__fields' },
        h(TextField, { label: 'Username', name: 'username', autoComplete: 'username', value: username, onChange: setUsername, inputRef: userRef,
          validationState: errors.username ? 'invalid' : undefined, errorMessage: errors.username, isRequired: true }),
        h(TextField, { label: 'Password', name: 'password', type: 'password', autoComplete: 'current-password', revealable: true, value: password, onChange: setPassword, inputRef: passRef,
          validationState: errors.password ? 'invalid' : undefined, errorMessage: errors.password, isRequired: true })),
      h(Button, { variant: 'accent', type: 'submit', size: 'l', isDisabled: busy }, busy ? 'Signing in…' : 'Sign in')));
}
