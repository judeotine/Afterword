import "@blocknote/core/fonts/inter.css";
import { useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import "@blocknote/shadcn/style.css";
import { ChangeEvent, useCallback, useEffect } from "react";

const initialMarkdown = "Hello, **world!**";

export default function BasicBlockNoteTest() {
  const editor = useCreateBlockNote({});

  const markdownInputChanged = useCallback(
    async (e: ChangeEvent<HTMLTextAreaElement>) => {
      const blocks = await editor.tryParseMarkdownToBlocks(e.target.value);
      editor.replaceBlocks(editor.document, blocks);
    },
    [editor],
  );

  useEffect(() => {
    async function loadInitialHTML() {
      const blocks = await editor.tryParseMarkdownToBlocks(initialMarkdown);
      editor.replaceBlocks(editor.document, blocks);
    }
    loadInitialHTML();
  }, [editor]);

  return (
    <div className="views">
      <div className="view-wrapper">
        <div className="view-label">Markdown Input</div>
        <div className="view">
          <code>
            <textarea
              defaultValue={initialMarkdown}
              onChange={markdownInputChanged}
            />
          </code>
        </div>
      </div>
      <div className="view-wrapper">
        <div className="view-label">Editor Output</div>
        <div className="view">
          <BlockNoteView editor={editor} editable={true} />
        </div>
      </div>
    </div>
  );
}
