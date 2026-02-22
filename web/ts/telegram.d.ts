interface TelegramHapticFeedback {
  impactOccurred(style: "light" | "medium" | "heavy" | "rigid" | "soft"): void;
  notificationOccurred(style: "error" | "success" | "warning"): void;
  selectionChanged(): void;
}

interface TelegramWebAppUser {
  id: number;
  is_bot?: boolean;
  first_name: string;
  last_name?: string;
  username?: string;
}

interface TelegramWebApp {
  ready(): void;
  expand(): void;
  initData: string;
  initDataUnsafe: {
    user?: TelegramWebAppUser;
  };
  HapticFeedback: TelegramHapticFeedback;
}

interface Window {
  Telegram?: {
    WebApp?: TelegramWebApp;
  };
}
