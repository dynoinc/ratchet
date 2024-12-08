import '../app/globals.css';
import ErrorBoundary from './ErrorBoundary';

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-gray-50">
      <ErrorBoundary>
        {children}
      </ErrorBoundary>
    </div>
  );
} 