// @overleaf/web-frontend — ide-react/collab: the D19/D20 Yjs client engine
// (room content, offline persistence, network sync, CM6 bridge).
export {
  createEngine,
  type CreateEngineOptions,
  type YjsEngine,
  TEXT_TYPE,
} from "./engine";
export { TEXT_TYPE as Y_TEXT_TYPE } from "./text-type";
export { syncExtension } from "./codemirror";
export { spanBodies, type TrackedChangeBody } from "./capture";
export {
  attachProviders,
  collabEndpoint,
  wsBaseUrl,
  type CollabEndpoint,
  type Providers,
} from "./providers";
export {
  LOCAL_ORIGIN,
  REMOTE_ORIGIN,
  YTextSync,
  type LocalChange,
  type TextSink,
} from "./sync";
export { newYContent, type YContent } from "./ydoc";
