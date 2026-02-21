'use client';

import Image from 'next/image';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useSession } from 'next-auth/react';
import { cn } from '@/lib/utils';
import {
  LayoutDashboard,
  Users,
  CheckSquare,
  FileText,
  BarChart3,
  Settings,
  Box,
  Shield,
  GitGraph,
  UserCog,
  User,
} from 'lucide-react';

const navigation = [
  { name: 'Dashboard', href: '/', icon: LayoutDashboard },
  { name: 'Sessions', href: '/sessions', icon: Users },
  { name: 'Approvals', href: '/approvals', icon: CheckSquare },
  { name: 'Audit', href: '/audit', icon: FileText },
  { name: 'Lineage', href: '/lineage', icon: GitGraph },
  { name: 'Reports', href: '/reports', icon: BarChart3 },
  { name: 'Services', href: '/services', icon: Box },
  { name: 'Policies', href: '/policies', icon: Shield },
  { name: 'Settings', href: '/settings', icon: Settings },
];

const adminNavigation = [
  { name: 'Users', href: '/users', icon: UserCog },
];

const accountNavigation = [
  { name: 'Profile', href: '/profile', icon: User },
];

export function Sidebar() {
  const pathname = usePathname();
  const { data: session } = useSession();
  const isAdmin = session?.user?.role === 'admin';

  const renderNavItem = (item: { name: string; href: string; icon: any }) => {
    const isActive = pathname === item.href ||
      (item.href !== '/' && pathname.startsWith(item.href));
    return (
      <Link
        key={item.name}
        href={item.href}
        className={cn(
          'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
          isActive
            ? 'bg-primary text-primary-foreground'
            : 'text-muted-foreground hover:bg-muted hover:text-foreground'
        )}
      >
        <item.icon className="h-5 w-5" />
        {item.name}
      </Link>
    );
  };

  return (
    <div className="flex h-full w-64 flex-col bg-card border-r">
      {/* Logo */}
      <div className="flex h-16 items-center border-b px-6">
        <Link href="/" className="flex items-center gap-3">
          <Image
            src="/logo.png"
            alt="ADP Logo"
            width={36}
            height={36}
            className="rounded-full"
          />
          <span className="text-lg font-semibold">ADP</span>
        </Link>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 p-4">
        {navigation.map(renderNavItem)}

        {/* Admin section */}
        {isAdmin && (
          <>
            <div className="pt-4 pb-1">
              <span className="px-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Admin
              </span>
            </div>
            {adminNavigation.map(renderNavItem)}
          </>
        )}

        {/* Account section */}
        <div className="pt-4 pb-1">
          <span className="px-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Account
          </span>
        </div>
        {accountNavigation.map(renderNavItem)}
      </nav>

      {/* Footer */}
      <div className="border-t p-4">
        <div className="text-xs text-muted-foreground">
          Agent Developer Portal
          <br />
          v1.0.0
        </div>
      </div>
    </div>
  );
}
