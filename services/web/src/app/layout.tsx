import type { Metadata } from 'next';
import './globals.css';
import { SessionProvider } from '@/lib/session';

export const metadata: Metadata = {
  title: 'Afterword',
  description: 'Meeting recordings, transcripts and summaries',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <SessionProvider>{children}</SessionProvider>
      </body>
    </html>
  );
}
