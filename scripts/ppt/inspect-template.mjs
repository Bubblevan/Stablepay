import fs from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

function usage() {
  return [
    "Usage:",
    "  node scripts/ppt/inspect-template.mjs --workspace <dir> --pptx <source.pptx>",
  ].join("\n");
}

function listSlides(presentation) {
  if (Array.isArray(presentation.slides?.items)) return presentation.slides.items;
  if (Number.isInteger(presentation.slides?.count) && typeof presentation.slides.getItem === "function") {
    return Array.from({ length: presentation.slides.count }, (_, index) => presentation.slides.getItem(index));
  }
  throw new Error("Could not enumerate slides.");
}

async function main() {
  const utilsModule = await import(
    pathToFileURL(
      "C:/Users/bubblevan/.codex/plugins/cache/openai-primary-runtime/presentations/26.630.12135/skills/presentations/container_tools/artifact_tool_utils.mjs",
    ).href
  );
  const {
    ensureArtifactToolWorkspace,
    importArtifactTool,
    parseArgs,
    requireArg,
    saveBlobToFile,
  } = utilsModule;

  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    console.log(usage());
    return;
  }

  const workspaceDir = path.resolve(requireArg(args, "workspace"));
  const pptxPath = path.resolve(requireArg(args, "pptx"));
  const outDir = path.join(workspaceDir, "template-inspect-lite");
  const slidesDir = path.join(outDir, "source-slides");
  const layoutsDir = path.join(outDir, "layouts");

  await ensureArtifactToolWorkspace(workspaceDir);
  const { FileBlob, PresentationFile } = await importArtifactTool(workspaceDir);

  await fs.rm(outDir, { recursive: true, force: true });
  await fs.mkdir(slidesDir, { recursive: true });
  await fs.mkdir(layoutsDir, { recursive: true });

  const presentation = await PresentationFile.importPptx(await FileBlob.load(pptxPath));
  const slides = listSlides(presentation);

  const inspect = await presentation.inspect({
    kind: "slide,textbox,shape,image,table,chart,layout",
    maxChars: 200000,
  });
  await fs.writeFile(path.join(outDir, "template-inspect.ndjson"), inspect.ndjson || "", "utf8");

  for (let index = 0; index < slides.length; index += 1) {
    const slide = slides[index];
    const n = String(index + 1).padStart(2, "0");
    const preview = await presentation.export({ slide, format: "png", scale: 1 });
    await saveBlobToFile(preview, path.join(slidesDir, `source-slide-${n}.png`));

    const layout = await slide.export({ format: "layout" });
    await fs.writeFile(path.join(layoutsDir, `source-slide-${n}.layout.json`), await layout.text(), "utf8");
  }

  const montage = await presentation.export({ format: "webp", montage: true, scale: 1 });
  await saveBlobToFile(montage, path.join(outDir, "template-montage.webp"));

  await fs.writeFile(
    path.join(outDir, "template-manifest.json"),
    `${JSON.stringify({ sourcePptx: pptxPath, slideCount: slides.length }, null, 2)}\n`,
    "utf8",
  );

  console.log(outDir);
}

main().catch((error) => {
  console.error(error.stack || error.message || String(error));
  console.error(usage());
  process.exit(1);
});
