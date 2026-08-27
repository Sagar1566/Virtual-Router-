import type { ReactNode } from 'react';
import {
  Box,
  Chip,
  ListItem,
  ListItemIcon,
  ListItemText,
  Typography,
  type ChipProps,
} from '@mui/material';

export interface SetupListItemChip {
  label: string;
  color?: ChipProps['color'];
  variant?: ChipProps['variant'];
}

interface SetupListItemProps {
  icon: ReactNode;
  name: string;
  chips?: SetupListItemChip[];
  versionText?: string;
  secondary?: ReactNode;
  onClick?: () => void;
  endAction?: ReactNode;
  monospace?: boolean;
}

export function SetupListItem({
  icon,
  name,
  chips = [],
  versionText,
  secondary,
  onClick,
  endAction,
  monospace,
}: SetupListItemProps) {
  return (
    <ListItem
      sx={{
        cursor: onClick ? 'pointer' : 'default',
        px: { xs: 0.5, sm: 2 },
      }}
      onClick={onClick}
    >
      <ListItemIcon sx={{ minWidth: { xs: 36, sm: 56 } }}>
        {icon}
      </ListItemIcon>
      <ListItemText
        sx={{ mr: endAction ? 4 : 0 }}
        primary={
          <Box display="flex" alignItems="center" gap={0.5}>
            <Typography
              component="span"
              fontWeight="medium"
              noWrap
              sx={{
                fontSize: { xs: '0.875rem', sm: '1rem' },
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                flex: 1,
                minWidth: 60,
                fontFamily: monospace ? 'monospace' : undefined,
              }}
            >
              {name}
            </Typography>
            {chips.map((chip, idx) => (
              <Chip
                key={idx}
                label={chip.label}
                color={chip.color}
                size="small"
                variant={chip.variant}
              />
            ))}
            {versionText && (
              <Typography variant="caption" color="text.secondary" noWrap>
                {versionText}
              </Typography>
            )}
          </Box>
        }
        secondary={
          typeof secondary === 'string' ? (
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{
                fontSize: { xs: '0.75rem', sm: '0.875rem' },
                wordBreak: 'break-word',
              }}
            >
              {secondary}
            </Typography>
          ) : (
            secondary
          )
        }
      />
      {endAction}
    </ListItem>
  );
}
