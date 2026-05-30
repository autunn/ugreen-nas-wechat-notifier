import { copyFileSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..");
const sourceDir = path.join(repoRoot, "frontend", "ugreen-app");
const targetDir = path.join(repoRoot, "packaging", "ugreen-native-app", "rootfs_common", "www");

function ensureDir(dir) {
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
}

function cleanDir(dir) {
  ensureDir(dir);
  for (const entry of readdirSync(dir)) {
    rmSync(path.join(dir, entry), { recursive: true, force: true });
  }
}

cleanDir(targetDir);

const configuredBuildStamp = process.env.UGREEN_FRONTEND_BUILD_STAMP?.trim();
const buildStamp = configuredBuildStamp
  ? configuredBuildStamp.replace(/[^0-9A-Za-z._-]/g, "-")
  : new Date()
      .toISOString()
      .replace(/[-:]/g, "")
      .replace(/\.\d+Z$/, "Z");

const sourceIndexPath = path.join(sourceDir, "index.html");
const targetSrcDir = path.join(targetDir, "src");

ensureDir(targetSrcDir);

const cssName = `styles-${buildStamp}.css`;
const jsName = `main-${buildStamp}.js`;

copyFileSync(path.join(sourceDir, "src", "styles.css"), path.join(targetSrcDir, cssName));
copyFileSync(path.join(sourceDir, "src", "main.js"), path.join(targetSrcDir, jsName));

const builtIndex = readFileSync(sourceIndexPath, "utf8")
  .replace("./src/styles.css", `./src/${cssName}`)
  .replace("./src/main.js", `./src/${jsName}`);

writeFileSync(path.join(targetDir, "index.html"), builtIndex, "utf8");

console.log(`UGREEN frontend assets synced to ${targetDir}`);
