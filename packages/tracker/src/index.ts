// Entrada do script publicado em /s/vX.Y.Z/n.js.
import { start } from "./tracker.ts";

start(document.currentScript as HTMLScriptElement | null);
