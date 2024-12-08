import Link from 'next/link';

export default function BackButton() {
  return (
    <Link 
      href="/"
      className="inline-flex items-center text-blue-600 hover:text-blue-700 mb-6"
    >
      <svg 
        className="w-4 h-4 mr-1" 
        fill="none" 
        stroke="currentColor" 
        viewBox="0 0 24 24"
      >
        <path 
          strokeLinecap="round" 
          strokeLinejoin="round" 
          strokeWidth={2} 
          d="M15 19l-7-7 7-7" 
        />
      </svg>
      Back to Teams List
    </Link>
  );
} 