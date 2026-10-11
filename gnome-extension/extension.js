// Parent Tracker sensor: answers "which application is in front?" on the
// user's session bus, and nothing else.
//
// What it does not do: no network access, no files written, no screen contents,
// no keyboard or pointer input, and no memory of what it was asked. It does not
// watch focus changes; it looks at the focused window only when the
// parent-tracker-agent calls GetFocus. The window title is returned only while
// the agent has turned title capture on with SetTitleCapture (off by default).
//
// The bus name, path and method signatures must match internal/agent/gnome.go.

import Gio from 'gi://Gio';
import Shell from 'gi://Shell';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const BUS_NAME = 'org.parenttracker.Sensor';
const OBJECT_PATH = '/org/parenttracker/Sensor';

const INTERFACE_XML = `
<node>
  <interface name="org.parenttracker.Sensor">
    <method name="GetFocus">
      <arg type="b" direction="out" name="available"/>
      <arg type="s" direction="out" name="app_id"/>
      <arg type="s" direction="out" name="app_name"/>
      <arg type="u" direction="out" name="pid"/>
      <arg type="s" direction="out" name="title"/>
    </method>
    <method name="SetTitleCapture">
      <arg type="b" direction="in" name="enabled"/>
    </method>
  </interface>
</node>`;

class Sensor {
    constructor() {
        this._titles = false;
    }

    SetTitleCapture(enabled) {
        this._titles = Boolean(enabled);
    }

    GetFocus() {
        const win = global.display.focus_window;
        if (!win)
            return [false, '', '', 0, ''];

        const app = Shell.WindowTracker.get_default().get_window_app(win);
        let id = '';
        let name = '';
        if (app) {
            id = app.get_id() ?? '';
            name = app.get_name() ?? '';
        }
        if (!id)
            id = win.get_gtk_application_id() || win.get_wm_class() || '';
        if (!name)
            name = win.get_wm_class() || id;

        const title = this._titles ? (win.get_title() ?? '') : '';
        return [id !== '', id, name, Math.max(0, win.get_pid()), title];
    }
}

export default class ParentTrackerSensor extends Extension {
    enable() {
        this._sensor = new Sensor();
        this._exported = Gio.DBusExportedObject.wrapJSObject(INTERFACE_XML, this._sensor);
        this._exported.export(Gio.DBus.session, OBJECT_PATH);
        this._ownerId = Gio.bus_own_name_on_connection(
            Gio.DBus.session, BUS_NAME, Gio.BusNameOwnerFlags.NONE, null, null);
    }

    disable() {
        if (this._ownerId) {
            Gio.bus_unown_name(this._ownerId);
            this._ownerId = 0;
        }
        if (this._exported) {
            this._exported.unexport();
            this._exported = null;
        }
        this._sensor = null;
    }
}
