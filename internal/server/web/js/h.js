// React and Lumen come from the vendored UMD bundles (no build step). `h` is
// React.createElement, so views read as plain JavaScript.
export const React = window.React;
export const ReactDOM = window.ReactDOM;
export const L = window.Lumen;
export const h = React.createElement;
export const F = React.Fragment;
export const { useState, useEffect, useRef, useMemo, useCallback, useReducer } = React;
