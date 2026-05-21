type ClipboardNavigator = {
  clipboard?: {
    writeText(text: string): Promise<void>;
  };
};

export type CopyResult =
  | {
      copied: true;
    }
  | {
      copied: false;
      text: string;
    };

export async function copyText(text: string, clipboardNavigator: ClipboardNavigator = navigator): Promise<CopyResult> {
  try {
    await clipboardNavigator.clipboard?.writeText(text);
    if (clipboardNavigator.clipboard) {
      return { copied: true };
    }
  } catch {
    // Browsers commonly block clipboard access on HTTP origins.
  }
  return { copied: false, text };
}
