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

interface TelegramBackButton {
  isVisible: boolean;
  show(): TelegramBackButton;
  hide(): TelegramBackButton;
  onClick(callback: () => void): TelegramBackButton;
  offClick(callback: () => void): TelegramBackButton;
}

interface TelegramCloudStorage {
  setItem(key: string, value: string, callback?: (error: string | null, status: boolean) => void): void;
  getItem(key: string, callback?: (error: string | null, value: string | null) => void): void;
}

interface TelegramMainButton {
  text: string;
  color: string;
  textColor: string;
  isVisible: boolean;
  isProgressVisible: boolean;
  isActive: boolean;
  setText(text: string): TelegramMainButton;
  onClick(callback: () => void): TelegramMainButton;
  offClick(callback: () => void): TelegramMainButton;
  show(): TelegramMainButton;
  hide(): TelegramMainButton;
  enable(): TelegramMainButton;
  disable(): TelegramMainButton;
  showProgress(leaveActive: boolean): TelegramMainButton;
  hideProgress(): TelegramMainButton;
  setParams(params: {
    text?: string;
    color?: string;
    text_color?: string;
    is_active?: boolean;
    is_visible?: boolean;
  }): TelegramMainButton;
}

interface TelegramWebApp {
  ready(): void;
  expand(): void;
  initData: string;
  initDataUnsafe: {
    user?: TelegramWebAppUser;
  };
  HapticFeedback: TelegramHapticFeedback;
  MainButton: TelegramMainButton;
  BackButton?: TelegramBackButton;
  CloudStorage?: TelegramCloudStorage;
  setHeaderColor?(color: string): void;
  setBackgroundColor?(color: string): void;
  disableVerticalSwipes?(): void;
  enableClosingConfirmation?(): void;
}

interface Window {
  Telegram?: {
    WebApp?: TelegramWebApp;
  };
}
