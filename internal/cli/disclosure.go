package cli

// DisclosureText is the notice the administrator must acknowledge before
// anything is recorded (SEC-1, SEC-6). It is plain and factual, and it is the
// same notice the child can read with `parent-tracker self` and `notice`.
const DisclosureText = `Before Parent Tracker records anything, please read this.

What is recorded, on this computer only:
  - when each monitored account is signed in, and whether the session is
    active, idle, locked or asleep
  - which application is in front, and for how long (window titles only if
    you turn that on)
  - the same, hour by hour, and any time recording was interrupted

What is never recorded: the screen, keystrokes, messages or network traffic.

Who can see it:
  - You can open the dashboard and see the current state and the application
    in front live, and usage by hour of the day.
  - The monitored person can see everything recorded about them by running
    "parent-tracker self", including that you can see it live.

Use Parent Tracker only to look after your own child on a computer you own
and administer. Tell your child it is running. Do not use it on adults,
guests or computers you do not control.
`

// ChildNotice is shown in a monitored user's session (SEC-2).
const ChildNotice = `Parent Tracker is recording how this computer is used: when you are signed in,
which application is in front, and for how long. Your parent can see your
current state and the app in front, live, and your usage hour by hour. It never
records your screen, keystrokes or messages. Run "parent-tracker self" to see
exactly what is recorded about you.`
